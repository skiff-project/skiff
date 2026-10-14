package script

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"

	"skiff/internal/prompt"
)

// consolePrompter asks questions on the terminal, or answers them
// automatically in batch mode.
type consolePrompter struct {
	r       *Runner
	mu      sync.Mutex
	in      *bufio.Reader
	out     io.Writer
	hostKey string // "-hostkey=*" accepts any key
}

func (p *consolePrompter) interactive() bool {
	return p.r.batch == "off" && term.IsTerminal(int(os.Stdin.Fd()))
}

func (p *consolePrompter) readLine() (string, error) {
	s, err := p.in.ReadString('\n')
	return strings.TrimRight(s, "\r\n"), err
}

func (p *consolePrompter) readSecret() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(p.out)
		return string(b), err
	}
	return p.readLine()
}

func (p *consolePrompter) Prompt(ctx context.Context, req prompt.Request) (prompt.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch req.Kind {
	case prompt.HostKey:
		if p.hostKey == "*" || p.hostKey == "acceptnew" && req.Title == "Unknown server" {
			return prompt.Response{Choice: "once"}, nil
		}
	case prompt.Certificate:
		if p.r.opts["certificate"] == "*" {
			return prompt.Response{Choice: "once"}, nil
		}
	case prompt.Overwrite:
		if p.r.confirm == "off" || !p.interactive() {
			return prompt.Response{Choice: "overwrite", ApplyAll: true}, nil
		}
	case prompt.Error:
		if p.r.batch == "continue" {
			return prompt.Response{Choice: "skip"}, nil
		}
		if !p.interactive() {
			return prompt.Response{Choice: "abort"}, nil
		}
	case prompt.Banner:
		fmt.Fprintln(p.out, req.Message)
		return prompt.Response{Choice: "ok"}, nil
	}
	if !p.interactive() {
		switch req.Kind {
		case prompt.HostKey:
			return prompt.Response{}, fmt.Errorf("host key was not verified (%s %s); use -hostkey=\"%s\" to pin it", req.Details["keyType"], req.Details["sha256"], req.Details["sha256"])
		case prompt.Certificate:
			return prompt.Response{}, fmt.Errorf("certificate was not trusted (%s); use -certificate=\"%s\"", req.Message, req.Details["sha256"])
		}
		return prompt.Response{}, prompt.ErrNoUser
	}

	fmt.Fprintf(p.out, "\n%s\n", req.Title)
	if req.Message != "" {
		fmt.Fprintln(p.out, req.Message)
	}
	for _, k := range []string{"host", "keyType", "sha256", "md5", "previous", "subject", "issuer", "validTo", "target", "srcSize", "dstSize"} {
		if v, ok := req.Details[k]; ok && v != "" {
			fmt.Fprintf(p.out, "  %-10s %s\n", k+":", v)
		}
	}
	if len(req.Fields) > 0 {
		vals := make([]string, len(req.Fields))
		for i, f := range req.Fields {
			fmt.Fprintf(p.out, "%s: ", strings.TrimSuffix(f.Label, ":"))
			var err error
			if f.Secret {
				vals[i], err = p.readSecret()
			} else {
				vals[i], err = p.readLine()
			}
			if err != nil {
				return prompt.Response{}, prompt.ErrCancelled
			}
		}
		return prompt.Response{Choice: "ok", Values: vals}, nil
	}
	if len(req.Choices) == 0 {
		return prompt.Response{Choice: "ok"}, nil
	}
	for {
		var opts []string
		for i, c := range req.Choices {
			opts = append(opts, fmt.Sprintf("%d) %s", i+1, c.Label))
		}
		if req.ApplyAll {
			opts = append(opts, "(add * to apply to all, e.g. 1*)")
		}
		fmt.Fprintf(p.out, "%s\n> ", strings.Join(opts, "  "))
		line, err := p.readLine()
		if err != nil {
			return prompt.Response{}, prompt.ErrCancelled
		}
		line = strings.TrimSpace(line)
		all := strings.HasSuffix(line, "*")
		line = strings.TrimSuffix(line, "*")
		var n int
		if _, err := fmt.Sscanf(line, "%d", &n); err == nil && n >= 1 && n <= len(req.Choices) {
			return prompt.Response{Choice: req.Choices[n-1].ID, ApplyAll: all}, nil
		}
		for _, c := range req.Choices {
			if strings.EqualFold(line, c.ID) || strings.EqualFold(line, c.Label) {
				return prompt.Response{Choice: c.ID, ApplyAll: all}, nil
			}
		}
	}
}
