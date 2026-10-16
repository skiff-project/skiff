package core

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"skiff/internal/vfs"
)

// ptyBackend is an interactive terminal (SSH session or local PTY).
type ptyBackend interface {
	io.Reader
	io.Writer
	Resize(cols, rows int) error
	Close() error
	Wait() error
}

type sshPty struct {
	sess *ssh.Session
	in   io.WriteCloser
	out  io.Reader
}

func (s *sshPty) Read(p []byte) (int, error)  { return s.out.Read(p) }
func (s *sshPty) Write(p []byte) (int, error) { return s.in.Write(p) }
func (s *sshPty) Resize(cols, rows int) error { return s.sess.WindowChange(rows, cols) }
func (s *sshPty) Close() error                { s.in.Close(); return s.sess.Close() }
func (s *sshPty) Wait() error                 { return s.sess.Wait() }

// Terminal is one interactive shell.
type Terminal struct {
	ID      string    `json:"id"`
	Conn    string    `json:"conn"`
	Title   string    `json:"title"`
	Created time.Time `json:"created"`
	Exited  bool      `json:"exited"`

	be   ptyBackend
	mu   sync.Mutex
	buf  []byte // scrollback replay
	subs map[int]chan []byte
	next int
	core *Core
}

type terminals struct {
	core *Core
	mu   sync.Mutex
	m    map[string]*Terminal
}

func newTerminals(c *Core) *terminals { return &terminals{core: c, m: map[string]*Terminal{}} }

const replayMax = 256 * 1024

// OpenTerminal starts a shell on a connection ("local" for this computer).
func (c *Core) OpenTerminal(conn string, cols, rows int, cwd string) (*Terminal, error) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	term := c.Store.Prefs().Terminal.Term
	if term == "" {
		term = "xterm-256color"
	}
	var be ptyBackend
	var stderr io.Reader
	title := "Local"
	if conn == LocalID {
		if c.Local.Root != "" {
			// a shell cannot be confined to the --root folder
			return nil, errors.New("the local terminal is not available when the local panel is restricted (--root)")
		}
		lp, err := startLocalPty(cols, rows, cwd, term)
		if err != nil {
			return nil, err
		}
		be = lp
	} else {
		cn, err := c.Conn(conn)
		if err != nil {
			return nil, err
		}
		s := cn.SSH()
		if s == nil {
			return nil, errors.New("terminals are only available for SFTP and SCP connections")
		}
		title = cn.Title
		sess, err := s.NewSession()
		if err != nil {
			// the server may limit sessions per connection: free the idle
			// transfer channels and try again
			cn.closeIdle()
			if sess, err = s.NewSession(); err != nil {
				return nil, err
			}
		}
		modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
		if err := sess.RequestPty(term, rows, cols, modes); err != nil {
			sess.Close()
			return nil, err
		}
		in, err := sess.StdinPipe()
		if err != nil {
			sess.Close()
			return nil, err
		}
		out, err := sess.StdoutPipe()
		if err != nil {
			sess.Close()
			return nil, err
		}
		errp, _ := sess.StderrPipe()
		sess.Setenv("LANG", "en_US.UTF-8")
		if cwd != "" {
			err = sess.Start("cd " + vfs.ShellQuote(cwd) + " 2>/dev/null; exec \"${SHELL:-/bin/sh}\" -l")
		} else {
			err = sess.Shell()
		}
		if err != nil {
			sess.Close()
			return nil, err
		}
		be = &sshPty{sess: sess, in: in, out: out}
		stderr = errp
	}
	t := &Terminal{ID: randID(6), Conn: conn, Title: title, Created: time.Now(), be: be, subs: map[int]chan []byte{}, core: c}
	c.terms.mu.Lock()
	c.terms.m[t.ID] = t
	c.terms.mu.Unlock()
	go t.pump()
	if stderr != nil {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					t.emit(append([]byte(nil), buf[:n]...))
				}
				if err != nil {
					return
				}
			}
		}()
	}
	c.Bus.Publish("term.update", t.info())
	return t, nil
}

func (t *Terminal) info() Terminal {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Terminal{ID: t.ID, Conn: t.Conn, Title: t.Title, Created: t.Created, Exited: t.Exited}
}

func (t *Terminal) emit(p []byte) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > replayMax {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-replayMax*3/4:]...)
	}
	for _, ch := range t.subs {
		select {
		case ch <- p:
		default:
		}
	}
	t.mu.Unlock()
}

func (t *Terminal) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := t.be.Read(buf)
		if n > 0 {
			t.emit(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	t.be.Wait()
	t.mu.Lock()
	t.Exited = true
	for id, ch := range t.subs {
		close(ch)
		delete(t.subs, id)
	}
	t.mu.Unlock()
	t.core.Bus.Publish("term.update", t.info())
}

// Attach returns the scrollback and a live output channel.
func (t *Terminal) Attach() ([]byte, <-chan []byte, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	replay := append([]byte(nil), t.buf...)
	ch := make(chan []byte, 1024)
	if t.Exited {
		close(ch)
		return replay, ch, func() {}
	}
	id := t.next
	t.next++
	t.subs[id] = ch
	return replay, ch, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if c, ok := t.subs[id]; ok {
			delete(t.subs, id)
			close(c)
		}
	}
}

func (t *Terminal) Write(p []byte) error {
	_, err := t.be.Write(p)
	return err
}

func (t *Terminal) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return t.be.Resize(cols, rows)
}

func (c *Core) Terminal(id string) (*Terminal, error) {
	c.terms.mu.Lock()
	defer c.terms.mu.Unlock()
	t, ok := c.terms.m[id]
	if !ok {
		return nil, fmt.Errorf("terminal %s not found", id)
	}
	return t, nil
}

func (c *Core) Terminals() []Terminal {
	c.terms.mu.Lock()
	list := make([]*Terminal, 0, len(c.terms.m))
	for _, t := range c.terms.m {
		list = append(list, t)
	}
	c.terms.mu.Unlock()
	out := make([]Terminal, 0, len(list))
	for _, t := range list {
		out = append(out, t.info())
	}
	return out
}

func (c *Core) CloseTerminal(id string) {
	c.terms.mu.Lock()
	t, ok := c.terms.m[id]
	delete(c.terms.m, id)
	c.terms.mu.Unlock()
	if ok {
		t.be.Close()
		c.Bus.Publish("term.closed", id)
	}
}

func (ts *terminals) closeConn(conn string) {
	ts.mu.Lock()
	var ids []string
	for id, t := range ts.m {
		if t.Conn == conn {
			ids = append(ids, id)
		}
	}
	ts.mu.Unlock()
	for _, id := range ids {
		ts.core.CloseTerminal(id)
	}
}
