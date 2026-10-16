package tasks

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/prompt"
	"skiff/internal/script"
	"skiff/internal/transfer"
)

// logLines collects the lines written to it.
type logLines struct {
	mu    sync.Mutex
	lines []string
	part  string
	on    func(string)
}

func (l *logLines) add(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	on := l.on
	l.mu.Unlock()
	if on != nil {
		on(s)
	}
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	text := l.part + string(p)
	l.mu.Unlock()
	sc := bufio.NewScanner(strings.NewReader(text))
	var last string
	complete := strings.HasSuffix(text, "\n")
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if !complete && len(lines) > 0 {
		last, lines = lines[len(lines)-1], lines[:len(lines)-1]
	}
	l.mu.Lock()
	l.part = last
	l.mu.Unlock()
	for _, ln := range lines {
		l.add(ln)
	}
	return len(p), nil
}

func (l *logLines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]string(nil), l.lines...)
	if l.part != "" {
		out = append(out, l.part)
	}
	return out
}

// Execute runs a task to the end and returns its run (not saved). onLine,
// when set, receives each log line as it comes.
func Execute(ctx context.Context, cfg *config.Store, t Task, trigger string, onLine func(string)) Run {
	r := Run{ID: config.NewID(), TaskID: t.ID, TaskName: t.Name, Trigger: trigger, Start: time.Now(), Status: "running"}
	log := &logLines{on: onLine}
	var summary string
	var stats *RunStats
	var err error
	switch t.Kind {
	case "sync":
		summary, stats, err = runSync(ctx, cfg, t, log)
	case "script":
		summary, stats, err = runScript(ctx, cfg, t, log)
	default:
		err = fmt.Errorf("unknown kind of task %q", t.Kind)
	}
	r.End, r.Log = time.Now(), log.all()
	switch {
	case ctx.Err() != nil:
		r.Status, r.Summary = "cancelled", "Cancelled"
	case err != nil:
		r.Status, r.Summary = "failed", err.Error()
	default:
		r.Status, r.Summary, r.Stats = "ok", summary, stats
	}
	return r
}

// follow copies the log of a core into log.
func follow(c *core.Core, log *logLines) func() {
	events, unsub := c.Bus.Subscribe()
	go func() {
		for ev := range events {
			if ev.Type != "log" {
				continue
			}
			if m, ok := ev.Data.(map[string]any); ok {
				if ln, ok := m["line"].(core.LogLine); ok {
					log.add(ln.Time.Format("15:04:05") + " " + ln.Msg)
				}
			}
		}
	}()
	return unsub
}

func runSync(ctx context.Context, cfg *config.Store, t Task, log *logLines) (string, *RunStats, error) {
	c := core.New(cfg, core.Options{Prompter: prompt.Unattended})
	defer c.Shutdown()
	defer follow(c, log)()
	site, ok := cfg.Site(t.SiteID)
	if !ok {
		return "", nil, errors.New("the site of this task was deleted")
	}
	conn, err := c.Connect(ctx, t.SiteID, nil, "")
	if err != nil {
		return "", nil, err
	}
	req := t.Sync
	req.LocalConn, req.RemoteConn = core.LocalID, conn.ID
	if req.RemoteDir == "" {
		req.RemoteDir = conn.Info().Home
	}
	log.add(fmt.Sprintf("Comparing %s with %s:%s", req.LocalDir, site.Title(), req.RemoteDir))
	plan, err := c.SyncCompare(ctx, req)
	if err != nil {
		return "", nil, err
	}
	if len(plan.Actions) == 0 {
		return fmt.Sprintf("Already up to date (%d items compared)", plan.Scanned), &RunStats{Compared: plan.Scanned, UpToDate: true}, nil
	}
	res, err := c.SyncExecute(ctx, plan.ID, nil, nil)
	if err != nil {
		return "", nil, err
	}
	var files, failed int
	var bytes int64
	var errs []string
	for _, id := range res.Jobs {
		s := waitJob(ctx, c, id)
		files += s.DoneFiles
		failed += s.FailedFiles
		bytes += s.DoneBytes
		for _, e := range s.Errors {
			errs = append(errs, e.Path+": "+e.Error)
		}
		if s.Error != "" {
			errs = append(errs, s.Error)
		}
	}
	errs = append(errs, res.Errors...)
	for _, e := range errs {
		log.add("Error: " + e)
	}
	summary := fmt.Sprintf("%d file(s) transferred (%s), %d deleted", files, size(bytes), res.Deleted)
	if len(errs) > 0 || failed > 0 {
		return "", nil, fmt.Errorf("%s, %d error(s): %s", summary, max(failed, len(errs)), errs[0])
	}
	return summary, &RunStats{Files: files, Bytes: bytes, Deleted: res.Deleted, Compared: plan.Scanned}, ctx.Err()
}

func waitJob(ctx context.Context, c *core.Core, id string) transfer.Snapshot {
	for {
		for _, s := range c.Transfers.Jobs() {
			if s.ID == id {
				switch s.Status {
				case transfer.Done, transfer.Failed, transfer.Cancelled, transfer.Partial:
					return s
				}
			}
		}
		select {
		case <-ctx.Done():
			c.Transfers.Cancel(id)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func runScript(ctx context.Context, cfg *config.Store, t Task, log *logLines) (string, *RunStats, error) {
	r := script.NewRunner(log)
	r.SetBatch("abort")
	c := core.New(cfg, core.Options{Prompter: r.Prompter()})
	defer c.Shutdown()
	r.SetCore(c)
	lines := strings.Split(strings.ReplaceAll(t.Script, "\r\n", "\n"), "\n")
	err := r.RunLines(ctx, lines)
	if err != nil {
		return "", nil, err
	}
	n := 0
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" && !strings.HasPrefix(s, "#") {
			n++
		}
	}
	return fmt.Sprintf("Script finished (%d command(s))", n), &RunStats{Commands: n}, nil
}

var _ io.Writer = (*logLines)(nil)
