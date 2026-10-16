package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"skiff/internal/vfs"
)

// A log followed live: the end of a file, then what is added to it, or the
// journal of a service. With a shell, "tail -F" or "journalctl -f" run on
// the server; else the file is checked every second and its new end read.
// The text comes as "logs.data" events, the end as "logs.end".

// FollowRequest is a file or a service to follow.
type FollowRequest struct {
	Conn  string `json:"conn"`
	Path  string `json:"path,omitempty"`
	Unit  string `json:"unit,omitempty"` // a systemd service
	Lines int    `json:"lines,omitempty"`
}

const (
	followBatch   = 150 * time.Millisecond
	followPoll    = time.Second
	followMaxRead = 1 << 20
	followTailMax = 256 << 10
)

type followers struct {
	mu sync.Mutex
	m  map[string]context.CancelFunc
}

// Follow starts following a file or a service, and returns its id.
func (c *Core) Follow(req FollowRequest) (string, error) {
	if req.Lines <= 0 || req.Lines > 5000 {
		req.Lines = 200
	}
	if req.Unit != "" && (!serviceName.MatchString(req.Unit) || strings.HasPrefix(req.Unit, "-")) {
		return "", errors.New("invalid service name")
	}
	if req.Unit == "" && req.Path == "" {
		return "", errors.New("nothing to follow")
	}
	cn, shell := c.hasShell(req.Conn)
	if req.Unit != "" && !shell {
		return "", errors.New("the journal of a service needs a connection with a shell")
	}
	var f vfs.FS
	if !shell {
		var err error
		if f, err = c.FSFor(context.Background(), req.Conn); err != nil {
			return "", err
		}
		e, err := f.Stat(context.Background(), req.Path)
		if err != nil {
			return "", err
		}
		if e.IsDir {
			return "", vfs.ErrIsDir
		}
	}
	id := randID(6)
	ctx, cancel := context.WithCancel(context.Background())
	c.follow.mu.Lock()
	if c.follow.m == nil {
		c.follow.m = map[string]context.CancelFunc{}
	}
	c.follow.m[id] = cancel
	c.follow.mu.Unlock()
	out := &followWriter{id: id, c: c}
	go func() {
		var err error
		if shell {
			err = cn.execStream(ctx, followCommand(req), out)
		} else {
			err = c.pollFile(ctx, f, req.Path, req.Lines, out)
		}
		out.flush()
		msg := ""
		if err != nil && ctx.Err() == nil {
			msg = err.Error()
		}
		c.follow.mu.Lock()
		delete(c.follow.m, id)
		c.follow.mu.Unlock()
		cancel()
		c.Bus.Publish("logs.end", map[string]string{"id": id, "error": msg})
	}()
	return id, nil
}

// StopFollow stops following.
func (c *Core) StopFollow(id string) {
	c.follow.mu.Lock()
	cancel := c.follow.m[id]
	c.follow.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func followCommand(req FollowRequest) string {
	n := strconv.Itoa(req.Lines)
	if req.Unit != "" {
		return "journalctl -u " + req.Unit + " -n " + n + " -f --no-pager -o short-iso 2>&1"
	}
	p := vfs.ShellQuote(req.Path)
	// -F follows the name across rotations; a tail without it stops at
	// once, and -f is used instead
	return "tail -n " + n + " -F -- " + p + " 2>&1 || tail -n " + n + " -f -- " + p + " 2>&1"
}

// pollFile sends the last lines of a file, then what is added to it.
func (c *Core) pollFile(ctx context.Context, f vfs.FS, p string, lines int, out *followWriter) error {
	e, err := f.Stat(ctx, p)
	if err != nil {
		return err
	}
	// the last lines
	start := e.Size - followTailMax
	if start < 0 {
		start = 0
	}
	data, err := readFrom(ctx, f, p, start, e.Size-start)
	if err != nil {
		return err
	}
	if start > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	if all := strings.SplitAfter(string(data), "\n"); len(all) > lines+1 {
		data = []byte(strings.Join(all[len(all)-lines-1:], ""))
	}
	out.Write(data)
	out.flush()
	pos := e.Size
	t := time.NewTicker(followPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		e, err := f.Stat(ctx, p)
		if err != nil {
			continue // rotated: it comes back
		}
		if e.Size < pos {
			c.Bus.Publish("logs.note", map[string]string{"id": out.id, "kind": "truncated"})
			pos = 0
		}
		if e.Size == pos {
			continue
		}
		n := e.Size - pos
		if n > followMaxRead {
			pos = e.Size - followMaxRead
			n = followMaxRead
		}
		data, err := readFrom(ctx, f, p, pos, n)
		if err != nil {
			continue
		}
		pos += int64(len(data))
		out.Write(data)
		out.flush()
	}
}

func readFrom(ctx context.Context, f vfs.FS, p string, off, n int64) ([]byte, error) {
	r, err := f.Open(ctx, p, off)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, n))
}

// followWriter sends the text in events, a few times a second, cut at
// the end of a line when it can.
type followWriter struct {
	id    string
	c     *Core
	mu    sync.Mutex
	buf   []byte
	timer *time.Timer
}

func (w *followWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > 64<<10 {
		w.sendLocked(false)
	} else if w.timer == nil {
		w.timer = time.AfterFunc(followBatch, w.flush)
	}
	w.mu.Unlock()
	return len(p), nil
}

func (w *followWriter) flush() {
	w.mu.Lock()
	w.sendLocked(true)
	w.mu.Unlock()
}

func (w *followWriter) sendLocked(all bool) {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	n := len(w.buf)
	if !all {
		if i := bytes.LastIndexByte(w.buf, '\n'); i >= 0 {
			n = i + 1
		}
	}
	// a character cut in two waits for its end
	if !utf8.Valid(w.buf[:n]) {
		m := n
		for i := 0; i < 3 && m > 0 && !utf8.Valid(w.buf[:m]); i++ {
			m--
		}
		if utf8.Valid(w.buf[:m]) && !all {
			n = m
		}
	}
	if n == 0 {
		return
	}
	text := decodeText(w.buf[:n], "").Content
	w.buf = append([]byte(nil), w.buf[n:]...)
	w.c.Bus.Publish("logs.data", map[string]string{"id": w.id, "text": text})
}
