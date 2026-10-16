// Package script implements a WinSCP-compatible scripting language so that
// existing WinSCP automation (winscp.com /script=..., /command ...) runs
// unchanged with "skiff run".
package script

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/mask"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
)

type session struct {
	conn *core.Conn
	cwd  string
}

// Runner executes script commands.
type Runner struct {
	c        *core.Core
	out      io.Writer
	sessions []*session
	cur      int
	lcwd     string

	batch         string // off, on, abort, continue
	confirm       string // on, off
	transferMode  string // binary, ascii, automatic
	echo          bool
	failOnNoMatch bool
	opts          map[string]string
	prompter      *consolePrompter
}

// NewRunner creates a runner; the core must use r.Prompter().
func NewRunner(out io.Writer) *Runner {
	wd, _ := os.Getwd()
	r := &Runner{out: out, cur: -1, lcwd: wd, batch: "off", confirm: "on", transferMode: "binary", opts: map[string]string{}}
	r.prompter = &consolePrompter{r: r, in: bufio.NewReader(os.Stdin), out: out}
	return r
}

// Prompter returns the console prompter to pass to core.New.
func (r *Runner) Prompter() *consolePrompter { return r.prompter }

func (r *Runner) SetCore(c *core.Core) { r.c = c }

// SetBatch sets the initial batch mode ("abort" for non-interactive runs).
func (r *Runner) SetBatch(mode string) { r.batch = mode }

func (r *Runner) printf(format string, a ...any) { fmt.Fprintf(r.out, format, a...) }

// ---------------------------------------------------------------------------

// Tokenize splits a command line WinSCP-style ("" escapes a quote).
func Tokenize(line string) []string {
	var out []string
	var cur strings.Builder
	inq, has := false, false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '"':
			if inq && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inq = !inq
				has = true
			}
		case (ch == ' ' || ch == '\t') && !inq:
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

var (
	envRe = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*|[0-9]+)%`) // %VAR%, and %1% for /parameter
	tsRe  = regexp.MustCompile(`%TIMESTAMP(?:([+-])(\d+)([YMDHNS]))?#([^%]*)%`)
)

// expand replaces %VAR% and %TIMESTAMP#yyyy-mm-dd% like WinSCP.
func expand(s string) string {
	s = tsRe.ReplaceAllStringFunc(s, func(m string) string {
		g := tsRe.FindStringSubmatch(m)
		t := time.Now()
		if g[1] != "" {
			n, _ := strconv.Atoi(g[2])
			if g[1] == "-" {
				n = -n
			}
			switch g[3] {
			case "Y":
				t = t.AddDate(n, 0, 0)
			case "M":
				t = t.AddDate(0, n, 0)
			case "D":
				t = t.AddDate(0, 0, n)
			case "H":
				t = t.Add(time.Duration(n) * time.Hour)
			case "N":
				t = t.Add(time.Duration(n) * time.Minute)
			case "S":
				t = t.Add(time.Duration(n) * time.Second)
			}
		}
		f := strings.NewReplacer("yyyy", "2006", "yy", "06", "mm", "01", "dd", "02", "hh", "15", "nn", "04", "ss", "05", "zzz", "000").Replace(g[4])
		return t.Format(f)
	})
	return envRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return m
	})
}

// splitSwitches separates "-switch[=value]" arguments from the others.
func splitSwitches(args []string) (map[string]string, []string) {
	sw := map[string]string{}
	var rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") && len(a) > 1 && !strings.HasPrefix(a, "--") && !isNumber(a) {
			k, v, _ := strings.Cut(a[1:], "=")
			sw[strings.ToLower(k)] = v
			continue
		}
		rest = append(rest, a)
	}
	return sw, rest
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// ---------------------------------------------------------------------------

// RunLines executes a script; errors abort unless "option batch continue".
func (r *Runner) RunLines(ctx context.Context, lines []string) error {
	var firstErr error
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if r.echo {
			r.printf("> %s\n", line)
		}
		err := r.Exec(ctx, line)
		if errors.Is(err, errExit) {
			return firstErr
		}
		if err != nil {
			r.printf("Error (line %d): %v\n", i+1, err)
			if r.batch == "continue" {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			return err
		}
	}
	return firstErr
}

var errExit = errors.New("exit")

// Interactive runs a read-eval-print loop.
func (r *Runner) Interactive(ctx context.Context) error {
	in := r.prompter.in
	for {
		prompt := "skiff> "
		if s := r.session(); s != nil {
			prompt = fmt.Sprintf("%s:%s> ", s.conn.Title, s.cwd)
		}
		r.printf("%s", prompt)
		line, err := in.ReadString('\n')
		if err != nil {
			r.printf("\n")
			return nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := r.Exec(ctx, line); err != nil {
			if errors.Is(err, errExit) {
				return nil
			}
			r.printf("Error: %v\n", err)
		}
	}
}

func (r *Runner) session() *session {
	if r.cur < 0 || r.cur >= len(r.sessions) {
		return nil
	}
	return r.sessions[r.cur]
}

func (r *Runner) need() (*session, error) {
	s := r.session()
	if s == nil {
		return nil, errors.New("no session open; use the \"open\" command first")
	}
	return s, nil
}

func (r *Runner) rpath(s *session, p string) string {
	if p == "" {
		return s.cwd
	}
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		return path.Join(s.conn.Info().Home, strings.TrimPrefix(p, "~"))
	}
	return path.Join(s.cwd, p)
}

func (r *Runner) lpath(p string) string {
	if p == "" {
		return r.lcwd
	}
	if strings.HasPrefix(p, "~") {
		h, _ := os.UserHomeDir()
		p = filepath.Join(h, p[1:])
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(r.lcwd, p)
}

// Exec runs one command line.
func (r *Runner) Exec(ctx context.Context, line string) error {
	args := Tokenize(expand(line))
	if len(args) == 0 {
		return nil
	}
	cmd, args := strings.ToLower(args[0]), args[1:]
	switch cmd {
	case "exit", "bye", "quit":
		for len(r.sessions) > 0 {
			r.closeSession(0)
		}
		return errExit
	case "help", "?":
		r.help()
		return nil
	case "echo":
		r.printf("%s\n", strings.Join(args, " "))
		return nil
	case "option":
		return r.option(args)
	case "open":
		return r.open(ctx, args)
	case "close":
		return r.close(args)
	case "session":
		return r.sessionCmd(args)
	case "pwd":
		s, err := r.need()
		if err != nil {
			return err
		}
		r.printf("%s\n", s.cwd)
		return nil
	case "cd":
		return r.cd(ctx, args)
	case "ls", "dir":
		return r.ls(ctx, args)
	case "lpwd":
		r.printf("%s\n", r.lcwd)
		return nil
	case "lcd":
		if len(args) == 0 {
			return errors.New("lcd requires a directory")
		}
		p := r.lpath(args[0])
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			return fmt.Errorf("%s is not a directory", p)
		}
		r.lcwd = p
		r.printf("%s\n", p)
		return nil
	case "lls", "ldir":
		return r.lls(args)
	case "get":
		return r.transfer(ctx, args, false)
	case "put":
		return r.transfer(ctx, args, true)
	case "mkdir":
		return r.simple(ctx, args, 1, func(s *session, a []string) error {
			return r.c.Mkdir(ctx, s.conn.ID, r.rpath(s, a[0]), 0)
		})
	case "rm", "rmdir", "del":
		return r.rm(ctx, args)
	case "mv", "rename":
		return r.simple(ctx, args, 2, func(s *session, a []string) error {
			src, dst := r.rpath(s, a[0]), r.rpath(s, a[1])
			if e, err := r.c.Stat(ctx, s.conn.ID, dst); err == nil && e.IsDir {
				dst = path.Join(dst, path.Base(src))
			}
			return r.c.Rename(ctx, s.conn.ID, src, dst)
		})
	case "cp":
		return r.simple(ctx, args, 2, func(s *session, a []string) error {
			src, dst := r.rpath(s, a[0]), r.rpath(s, a[1])
			dir, names := dst, map[string]string{}
			if e, err := r.c.Stat(ctx, s.conn.ID, dst); err != nil || !e.IsDir {
				dir = path.Dir(dst)
				names[src] = path.Base(dst)
			}
			id, err := r.c.Duplicate(ctx, s.conn.ID, []string{src}, dir, names)
			if err != nil || id == "" {
				return err
			}
			return r.waitJob(ctx, id)
		})
	case "chmod":
		return r.chmod(ctx, args)
	case "ln", "symlink":
		sw, rest := splitSwitches(args)
		return r.simple(ctx, rest, 2, func(s *session, a []string) error {
			_, sym := sw["symbolic"]
			hard := !sym && cmd == "ln" && len(sw) > 0
			target := a[0]
			if hard {
				// a hard link names an existing file: resolve it like any path
				// (a symbolic link target is stored as written)
				target = r.rpath(s, target)
			}
			return r.c.Link(ctx, s.conn.ID, target, r.rpath(s, a[1]), hard)
		})
	case "stat":
		return r.simple(ctx, args, 1, func(s *session, a []string) error {
			e, err := r.c.Stat(ctx, s.conn.ID, r.rpath(s, a[0]))
			if err != nil {
				return err
			}
			r.printf("%s\n", formatEntry(e))
			return nil
		})
	case "checksum":
		return r.simple(ctx, args, 2, func(s *session, a []string) error {
			sum, err := r.c.Checksum(ctx, s.conn.ID, r.rpath(s, a[1]), a[0])
			if err != nil {
				return err
			}
			r.printf("%s  %s\n", sum, a[1])
			return nil
		})
	case "call", "!":
		s, err := r.need()
		if err != nil {
			return err
		}
		res, err := r.c.Exec(ctx, s.conn.ID, strings.Join(args, " "), s.cwd)
		if err != nil {
			return err
		}
		r.printf("%s", res.Stdout)
		if res.Stderr != "" {
			r.printf("%s", res.Stderr)
		}
		if res.Code != 0 {
			return fmt.Errorf("command exited with code %d", res.Code)
		}
		return nil
	case "synchronize", "sync":
		return r.synchronize(ctx, args)
	case "keepuptodate":
		return r.keepUpToDate(ctx, args)
	}
	return fmt.Errorf("unknown command %q (type \"help\")", cmd)
}

func (r *Runner) simple(ctx context.Context, args []string, n int, fn func(*session, []string) error) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	if len(args) < n {
		return fmt.Errorf("expected %d argument(s)", n)
	}
	return fn(s, args)
}

func (r *Runner) option(args []string) error {
	if len(args) == 0 {
		r.printf("batch        %s\nconfirm      %s\ntransfer     %s\necho         %v\nfailonnomatch %v\n", r.batch, r.confirm, r.transferMode, r.echo, r.failOnNoMatch)
		return nil
	}
	name := strings.ToLower(args[0])
	val := ""
	if len(args) > 1 {
		val = strings.ToLower(args[1])
	}
	switch name {
	case "batch":
		if val != "off" && val != "on" && val != "abort" && val != "continue" {
			return errors.New("batch must be off, on, abort or continue")
		}
		if val == "on" {
			val = "abort"
		}
		r.batch = val
	case "confirm":
		r.confirm = val
	case "transfer":
		r.transferMode = val
	case "echo":
		r.echo = val == "on"
	case "failonnomatch":
		r.failOnNoMatch = val == "on"
	case "reconnecttime", "exclude", "include":
		r.opts[name] = val
	default:
		return fmt.Errorf("unknown option %q", name)
	}
	return nil
}

func (r *Runner) open(ctx context.Context, args []string) error {
	sw, rest := splitSwitches(args)
	if len(rest) == 0 {
		return errors.New("open requires a session URL or a site name")
	}
	target := rest[0]
	var site config.Site
	var siteID, password string
	// saved site by name (with optional folder path)?
	for _, st := range r.c.Store.Sites() {
		full := st.Title()
		if st.Folder != "" {
			full = st.Folder + "/" + full
		}
		if strings.EqualFold(full, target) || strings.EqualFold(st.Title(), target) {
			siteID = st.ID
			break
		}
	}
	if siteID == "" {
		var err error
		site, password, err = config.ParseURL(target)
		if err != nil {
			return err
		}
		site.UseAgent = true
	}
	if v, ok := sw["password"]; ok {
		password = v
	}
	r.prompter.hostKey = ""
	if v, ok := sw["hostkey"]; ok {
		if v == "*" || strings.EqualFold(v, "acceptnew") {
			r.prompter.hostKey = v
		} else {
			site.HostKey = v
		}
	}
	if v, ok := sw["certificate"]; ok {
		if v == "*" {
			r.opts["certificate"] = "*"
		} else {
			site.TLSFingerprint = v
		}
	}
	if v, ok := sw["privatekey"]; ok {
		site.KeyFile = v
	}
	if v, ok := sw["passphrase"]; ok {
		site.Passphrase = v
	}
	if v, ok := sw["passive"]; ok {
		site.FTPActive = v == "off" || v == "0"
	}
	if _, ok := sw["explicit"]; ok {
		site.FTPS = "explicit"
	}
	if _, ok := sw["implicit"]; ok {
		site.FTPS = "implicit"
	}
	if v, ok := sw["timeout"]; ok {
		site.Timeout, _ = strconv.Atoi(v)
	}
	if v, ok := sw["rawsettings"]; ok {
		_ = v // accepted for compatibility
	}
	var conn *core.Conn
	var err error
	if siteID != "" {
		conn, err = r.c.Connect(ctx, siteID, nil, password)
	} else {
		r.printf("Searching for host...\nConnecting to host...\n")
		conn, err = r.c.Connect(ctx, "", &site, password)
	}
	if err != nil {
		return err
	}
	info := conn.Info()
	start := info.Home
	if info.RemoteDir != "" && info.Protocol != config.ProtoWebDAV {
		start = info.RemoteDir
	}
	r.sessions = append(r.sessions, &session{conn: conn, cwd: start})
	r.cur = len(r.sessions) - 1
	r.printf("Session started (%s).\nActive session: [%d] %s\n", strings.ToUpper(info.Protocol), r.cur+1, conn.Title)
	return nil
}

func (r *Runner) closeSession(i int) {
	s := r.sessions[i]
	s.conn.Close()
	r.sessions = append(r.sessions[:i], r.sessions[i+1:]...)
	if r.cur >= len(r.sessions) {
		r.cur = len(r.sessions) - 1
	}
}

func (r *Runner) close(args []string) error {
	if len(r.sessions) == 0 {
		return errors.New("no session open")
	}
	i := r.cur
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 || n > len(r.sessions) {
			return errors.New("invalid session number")
		}
		i = n - 1
	}
	title := r.sessions[i].conn.Title
	r.closeSession(i)
	r.printf("Session '%s' closed.\n", title)
	return nil
}

func (r *Runner) sessionCmd(args []string) error {
	if len(args) == 0 {
		for i, s := range r.sessions {
			mark := " "
			if i == r.cur {
				mark = "*"
			}
			r.printf("%s[%d] %s\n", mark, i+1, s.conn.Title)
		}
		return nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(r.sessions) {
		return errors.New("invalid session number")
	}
	r.cur = n - 1
	r.printf("Active session: [%d] %s\n", n, r.sessions[r.cur].conn.Title)
	return nil
}

func (r *Runner) cd(ctx context.Context, args []string) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	target := s.conn.Info().Home
	if len(args) > 0 {
		target = r.rpath(s, args[0])
	}
	e, err := r.c.Stat(ctx, s.conn.ID, target)
	if err != nil {
		return err
	}
	if !e.IsDir {
		return fmt.Errorf("%s is not a directory", target)
	}
	s.cwd = target
	r.printf("%s\n", target)
	return nil
}

func formatEntry(e vfs.Entry) string {
	typ := "-"
	if e.IsDir {
		typ = "d"
	}
	if e.IsLink {
		typ = "l"
	}
	perm := typ + permString(e.Mode)
	name := e.Name
	if e.IsLink && e.LinkTarget != "" {
		name += " -> " + e.LinkTarget
	}
	owner := e.Owner
	if owner == "" {
		owner = "-"
	}
	group := e.Group
	if group == "" {
		group = "-"
	}
	return fmt.Sprintf("%s %-8s %-8s %12d %s %s", perm, owner, group, e.Size, e.ModTime.Local().Format("Jan _2 15:04:05 2006"), name)
}

func permString(m uint32) string {
	const rwx = "rwxrwxrwx"
	b := []byte("---------")
	for i := 0; i < 9; i++ {
		if m&(1<<uint(8-i)) != 0 {
			b[i] = rwx[i]
		}
	}
	return string(b)
}

func (r *Runner) ls(ctx context.Context, args []string) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	dir, pattern := s.cwd, ""
	if len(args) > 0 {
		p := r.rpath(s, args[0])
		if strings.ContainsAny(path.Base(p), "*?[") {
			dir, pattern = path.Dir(p), path.Base(p)
		} else {
			dir = p
		}
	}
	lr, err := r.c.List(ctx, s.conn.ID, dir)
	if err != nil {
		return err
	}
	sort.Slice(lr.Entries, func(i, j int) bool { return lr.Entries[i].Name < lr.Entries[j].Name })
	n := 0
	for _, e := range lr.Entries {
		if pattern != "" && !mask.MustParse(pattern).Match(e.Name, e.Name, false, e.Size, e.ModTime) {
			continue
		}
		r.printf("%s\n", formatEntry(e))
		n++
	}
	if n == 0 && pattern != "" && r.failOnNoMatch {
		return fmt.Errorf("no file matching %q found", pattern)
	}
	return nil
}

func (r *Runner) lls(args []string) error {
	dir := r.lcwd
	if len(args) > 0 {
		dir = r.lpath(args[0])
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, de := range ents {
		fi, err := de.Info()
		if err != nil {
			continue
		}
		kind := "     "
		if fi.IsDir() {
			kind = "<DIR>"
		}
		r.printf("%s %12d %s %s\n", kind, fi.Size(), fi.ModTime().Format("2006-01-02 15:04:05"), de.Name())
	}
	return nil
}

func (r *Runner) rm(ctx context.Context, args []string) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("rm requires a file name")
	}
	var paths []string
	for _, a := range args {
		ps, err := r.expandRemote(ctx, s, a)
		if err != nil {
			return err
		}
		paths = append(paths, ps...)
	}
	if len(paths) == 0 {
		if r.failOnNoMatch {
			return errors.New("no file matching the mask")
		}
		return nil
	}
	for _, p := range paths {
		r.printf("Deleting %s\n", p)
	}
	return r.c.Delete(ctx, s.conn.ID, paths, false)
}

func (r *Runner) chmod(ctx context.Context, args []string) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	sw, rest := splitSwitches(args)
	if len(rest) < 2 {
		return errors.New("usage: chmod <mode> <file>...")
	}
	m, err := strconv.ParseUint(rest[0], 8, 32)
	if err != nil {
		return fmt.Errorf("invalid mode %q", rest[0])
	}
	mode := uint32(m)
	var paths []string
	for _, a := range rest[1:] {
		ps, err := r.expandRemote(ctx, s, a)
		if err != nil {
			return err
		}
		paths = append(paths, ps...)
	}
	_, rec := sw["recursive"]
	return r.c.SetAttrs(ctx, s.conn.ID, paths, core.Attrs{Mode: &mode, Recursive: rec})
}

// expandRemote expands a remote wildcard into matching paths.
func (r *Runner) expandRemote(ctx context.Context, s *session, arg string) ([]string, error) {
	p := r.rpath(s, arg)
	base := path.Base(p)
	if !strings.ContainsAny(base, "*?[") {
		return []string{p}, nil
	}
	lr, err := r.c.List(ctx, s.conn.ID, path.Dir(p))
	if err != nil {
		return nil, err
	}
	m := mask.MustParse(base)
	var out []string
	for _, e := range lr.Entries {
		if m.Match(e.Name, e.Name, false, e.Size, e.ModTime) {
			out = append(out, path.Join(path.Dir(p), e.Name))
		}
	}
	return out, nil
}

func (r *Runner) help() {
	r.printf(`Skiff script commands (WinSCP compatible):
  open <url|site> [-hostkey=fp|*] [-password=x] [-privatekey=f] [-passive=on|off] [-explicit|-implicit] [-certificate=fp|*]
  close [n]           session [n]          exit
  pwd   cd [dir]      ls [dir|mask]        lpwd   lcd <dir>   lls [dir]
  get [switches] <remote>... [local]       put [switches] <local>... [remote]
      -delete -resume -append -preservetime -nopreservetime -speed=KB -transfer=binary|ascii|automatic
      -filemask=mask -neweronly -latest -permissions=644
  mkdir <dir>   rm <file|mask>...   rmdir <dir>   mv <src> <dst>   cp <src> <dst>
  chmod <mode> <file>... [-recursive]   ln [-symbolic] <target> <link>   stat <file>
  checksum <md5|sha1|sha256|sha512> <file>   call <shell command>
  synchronize local|remote|both [-delete] [-mirror] [-criteria=time|size|either|checksum|none]
      [-filemask=mask] [-preview] [-transfer=mode] [local dir] [remote dir]
  keepuptodate [-delete] [-filemask=mask] [local dir] [remote dir]
  option [batch off|on|abort|continue] [confirm on|off] [transfer binary|ascii|automatic] [echo on|off] [failonnomatch on|off]
  echo <text>
Variables: %%ENVVAR%%, %%TIMESTAMP#yyyy-mm-dd%%, %%TIMESTAMP-1D#yyyymmdd%%
`)
}

// ---------------------------------------------------------------------------

func (r *Runner) transferOptions(sw map[string]string, src, dst string) transfer.Options {
	o := r.c.DefaultTransferOptions(src, dst)
	o.Overwrite = "ask"
	if r.confirm == "off" || r.batch != "off" {
		o.Overwrite = "overwrite"
	}
	mode := r.transferMode
	if v, ok := sw["transfer"]; ok {
		mode = strings.ToLower(v)
	}
	switch mode {
	case "ascii", "text":
		o.Mode = "text"
	case "automatic", "auto":
		o.Mode = "auto"
	default:
		o.Mode = "binary"
	}
	if _, ok := sw["delete"]; ok {
		o.Move = true
	}
	if _, ok := sw["resume"]; ok {
		o.Overwrite = "resume"
	}
	if _, ok := sw["append"]; ok {
		o.Overwrite = "resume"
	}
	if _, ok := sw["preservetime"]; ok {
		o.PreserveTime = true
	}
	if _, ok := sw["nopreservetime"]; ok {
		o.PreserveTime = false
	}
	if _, ok := sw["nopermissions"]; ok {
		o.PreservePerms, o.SetPerms = false, false
	}
	if v, ok := sw["permissions"]; ok {
		if m, err := strconv.ParseUint(v, 8, 32); err == nil {
			o.SetPerms, o.Perms = true, uint32(m)
		}
	}
	if v, ok := sw["speed"]; ok {
		o.SpeedLimit, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := sw["filemask"]; ok {
		o.FileMask = v
	}
	if _, ok := sw["neweronly"]; ok {
		o.NewerOnly = true
	}
	if r.batch == "continue" {
		o.ContinueOnError = true
	}
	return o
}

func (r *Runner) waitJob(ctx context.Context, id string) error {
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	var last transfer.Snapshot
	for {
		var snap *transfer.Snapshot
		for _, j := range r.c.Transfers.Jobs() {
			if j.ID == id {
				jj := j
				snap = &jj
			}
		}
		if snap == nil {
			return errors.New("transfer disappeared")
		}
		last = *snap
		switch snap.Status {
		case transfer.Done, transfer.Failed, transfer.Cancelled, transfer.Partial:
			if tty {
				r.printf("\r\033[K")
			}
			r.printf("%s | %d file(s), %s | %s/s | %s\n", snap.Label, snap.DoneFiles, human(snap.DoneBytes), human(int64(averageSpeed(last))), snap.Status)
			for _, e := range snap.Errors {
				r.printf("  error: %s: %s\n", e.Path, e.Error)
			}
			if snap.Status == transfer.Failed {
				return errors.New(snap.Error)
			}
			if snap.Status == transfer.Cancelled {
				return context.Canceled
			}
			if snap.FailedFiles > 0 {
				return fmt.Errorf("%d file(s) failed", snap.FailedFiles)
			}
			return nil
		}
		if tty {
			pct := 0.0
			if snap.TotalBytes > 0 {
				pct = float64(snap.DoneBytes) * 100 / float64(snap.TotalBytes)
			}
			cur := ""
			if len(snap.Active) > 0 {
				cur = snap.Active[0].Name
			}
			r.printf("\r\033[K%-30.30s | %s / %s | %s/s | %3.0f%%", cur, human(snap.DoneBytes), human(snap.TotalBytes), human(int64(snap.Speed)), pct)
		}
		select {
		case <-ctx.Done():
			r.c.Transfers.Cancel(id)
			return ctx.Err()
		case <-t.C:
		}
	}
}

func averageSpeed(s transfer.Snapshot) float64 {
	if s.Started.IsZero() {
		return 0
	}
	end := s.Finished
	if end.IsZero() {
		end = time.Now()
	}
	d := end.Sub(s.Started).Seconds()
	if d <= 0 {
		return 0
	}
	return float64(s.DoneBytes) / d
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// transfer implements get (upload=false) and put (upload=true).
func (r *Runner) transfer(ctx context.Context, args []string, upload bool) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	sw, rest := splitSwitches(args)
	if len(rest) == 0 {
		return errors.New("no files specified")
	}
	sources, target := rest, ""
	if len(rest) > 1 {
		sources, target = rest[:len(rest)-1], rest[len(rest)-1]
	}
	srcConn, dstConn := s.conn.ID, core.LocalID
	if upload {
		srcConn, dstConn = core.LocalID, s.conn.ID
	}
	var paths []string
	for _, a := range sources {
		if upload {
			p := r.lpath(a)
			if strings.ContainsAny(filepath.Base(p), "*?[") {
				ms, _ := filepath.Glob(p)
				paths = append(paths, ms...)
			} else {
				paths = append(paths, p)
			}
		} else {
			ps, err := r.expandRemote(ctx, s, a)
			if err != nil {
				return err
			}
			paths = append(paths, ps...)
		}
	}
	if _, latest := sw["latest"]; latest && len(paths) > 1 {
		var best string
		var bt time.Time
		for _, p := range paths {
			e, err := r.c.Stat(ctx, srcConn, p)
			if err == nil && !e.IsDir && e.ModTime.After(bt) {
				best, bt = p, e.ModTime
			}
		}
		if best != "" {
			paths = []string{best}
		}
	}
	if len(paths) == 0 {
		if r.failOnNoMatch {
			return errors.New("no file matching the mask")
		}
		r.printf("No file matching the mask found.\n")
		return nil
	}
	// Resolve the target directory / name.
	var dstDir string
	names := map[string]string{}
	if upload {
		t := r.rpath(s, target)
		if target == "" || strings.HasSuffix(target, "/") {
			dstDir = t
		} else if e, err := r.c.Stat(ctx, s.conn.ID, t); err == nil && e.IsDir {
			dstDir = t
		} else if len(paths) == 1 {
			dstDir, names[paths[0]] = path.Dir(t), path.Base(t)
		} else {
			dstDir = t
		}
	} else {
		t := r.lpath(target)
		if target == "" || strings.HasSuffix(target, "/") || strings.HasSuffix(target, `\`) {
			dstDir = t
		} else if st, err := os.Stat(t); err == nil && st.IsDir() {
			dstDir = t
		} else if len(paths) == 1 {
			dstDir, names[paths[0]] = filepath.Dir(t), filepath.Base(t)
		} else {
			dstDir = t
		}
	}
	o := r.transferOptions(sw, srcConn, dstConn)
	o.Parallel = 2
	id := r.c.Enqueue(transfer.Request{Src: srcConn, Dst: dstConn, Paths: paths, DstDir: dstDir, Names: names, Options: o})
	return r.waitJob(ctx, id)
}

func (r *Runner) synchronize(ctx context.Context, args []string) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	sw, rest := splitSwitches(args)
	if len(rest) == 0 {
		return errors.New("usage: synchronize local|remote|both [local dir] [remote dir]")
	}
	dir := strings.ToLower(rest[0])
	if dir != "local" && dir != "remote" && dir != "both" {
		return errors.New("direction must be local, remote or both")
	}
	ldir, rdir := r.lcwd, s.cwd
	if len(rest) > 1 {
		ldir = r.lpath(rest[1])
	}
	if len(rest) > 2 {
		rdir = r.rpath(s, rest[2])
	}
	req := core.SyncRequest{LocalConn: core.LocalID, LocalDir: ldir, RemoteConn: s.conn.ID, RemoteDir: rdir, Direction: dir, Mode: "standard", Criteria: "time"}
	if _, ok := sw["delete"]; ok {
		req.Delete = true
	}
	if _, ok := sw["mirror"]; ok {
		req.Mode = "mirror"
	}
	if v, ok := sw["criteria"]; ok {
		req.Criteria = strings.ToLower(v)
		if req.Criteria == "either" || req.Criteria == "time,size" || req.Criteria == "size,time" {
			req.Criteria = "either"
		}
	}
	if v, ok := sw["filemask"]; ok {
		req.FileMask = v
	}
	r.printf("Comparing...\nLocal '%s' %s Remote '%s'\n", ldir, map[string]string{"local": "<=", "remote": "=>", "both": "<=>"}[dir], rdir)
	plan, err := r.c.SyncCompare(ctx, req)
	if err != nil {
		return err
	}
	for _, e := range plan.Errors {
		r.printf("Warning: %s\n", e)
	}
	if len(plan.Actions) == 0 {
		r.printf("Nothing to synchronize.\n")
		return nil
	}
	for _, a := range plan.Actions {
		r.printf("  %-12s %s (%s)\n", a.Action, a.Rel, a.Reason)
	}
	if _, ok := sw["preview"]; ok {
		return nil
	}
	o := r.transferOptions(sw, core.LocalID, s.conn.ID)
	res, err := r.c.SyncExecute(ctx, plan.ID, nil, &o)
	if err != nil {
		return err
	}
	for _, id := range res.Jobs {
		if err := r.waitJob(ctx, id); err != nil {
			return err
		}
	}
	if res.Deleted > 0 {
		r.printf("Deleted %d item(s).\n", res.Deleted)
	}
	if len(res.Errors) > 0 {
		return errors.New(strings.Join(res.Errors, "; "))
	}
	return nil
}

func (r *Runner) keepUpToDate(ctx context.Context, args []string) error {
	s, err := r.need()
	if err != nil {
		return err
	}
	sw, rest := splitSwitches(args)
	ldir, rdir := r.lcwd, s.cwd
	if len(rest) > 0 {
		ldir = r.lpath(rest[0])
	}
	if len(rest) > 1 {
		rdir = r.rpath(s, rest[1])
	}
	req := core.WatchRequest{LocalDir: ldir, RemoteConn: s.conn.ID, RemoteDir: rdir, Recursive: true}
	if _, ok := sw["delete"]; ok {
		req.Delete = true
	}
	if v, ok := sw["filemask"]; ok {
		req.FileMask = v
	}
	events, unsub := r.c.Bus.Subscribe()
	defer unsub()
	w, err := r.c.StartWatch(req)
	if err != nil {
		return err
	}
	defer r.c.StopWatch(w.ID)
	r.printf("Watching for changes in '%s' → '%s', press Ctrl+C to abort...\n", ldir, rdir)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if ev.Type != "watch.event" {
				continue
			}
			m, _ := ev.Data.(map[string]any)
			we, _ := m["event"].(core.WatchEvent)
			if we.Error != "" {
				r.printf("%s %s: %s\n", we.Time.Format("15:04:05"), we.Path, we.Error)
			} else if we.Action != "info" {
				r.printf("%s %-7s %s\n", we.Time.Format("15:04:05"), we.Action, we.Path)
			}
		}
	}
}

// ReadScript loads a script file.
func ReadScript(p string) ([]string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\uFEFF")
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}
