package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"skiff/internal/vfs"
)

// ExternalEdit is a remote file opened in an external application; Skiff
// uploads it back automatically whenever it is saved.
type ExternalEdit struct {
	ID       string    `json:"id"`
	Conn     string    `json:"conn"`
	Remote   string    `json:"remote"`
	Local    string    `json:"local"`
	Editor   string    `json:"editor"`
	Opened   time.Time `json:"opened"`
	Uploaded time.Time `json:"uploaded,omitempty"`
	Uploads  int       `json:"uploads"`
	Error    string    `json:"error,omitempty"`

	w      *fsnotify.Watcher
	cancel context.CancelFunc
}

type externalEdits struct {
	core *Core
	mu   sync.Mutex
	m    map[string]*ExternalEdit
}

func newExternalEdits(c *Core) *externalEdits { return &externalEdits{core: c, m: map[string]*ExternalEdit{}} }

func editTempDir() string {
	return filepath.Join(os.TempDir(), "skiff-edit")
}

// splitCommand splits a command line honouring double quotes.
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for _, r := range s {
		switch {
		case r == '"':
			inq = !inq
		case (r == ' ' || r == '\t') && !inq:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// OpenWithDefault opens a local path with the operating system's default
// application.
func OpenWithDefault(p string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", p)
	case "darwin":
		cmd = exec.Command("open", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	return cmd.Start()
}

// RevealInFileManager shows a local file in the system file manager.
func RevealInFileManager(p string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", "/select,", p).Start()
	case "darwin":
		return exec.Command("open", "-R", p).Start()
	default:
		dir := p
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			dir = filepath.Dir(p)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func launchEditor(editor, file string) error {
	if strings.TrimSpace(editor) == "" {
		return OpenWithDefault(file)
	}
	args := splitCommand(editor)
	found := false
	for i, a := range args {
		if strings.Contains(a, "!.!") || strings.Contains(a, "%1") {
			args[i] = strings.NewReplacer("!.!", file, "%1", file).Replace(a)
			found = true
		}
	}
	if !found {
		args = append(args, file)
	}
	cmd := exec.Command(args[0], args[1:]...)
	return cmd.Start()
}

// EditExternal downloads a remote file, opens it in editor (system default
// when empty) and uploads it back on every save.
func (c *Core) EditExternal(ctx context.Context, conn, remote, editor string) (*ExternalEdit, error) {
	if c.Local.Root != "" {
		return nil, errors.New("external editors are not available in web mode")
	}
	if conn == LocalID {
		return nil, launchEditor(editor, remote)
	}
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return nil, err
	}
	// Reuse an existing edit of the same file.
	c.edits.mu.Lock()
	for _, e := range c.edits.m {
		if e.Conn == conn && e.Remote == remote {
			c.edits.mu.Unlock()
			return e, launchEditor(editor, e.Local)
		}
	}
	c.edits.mu.Unlock()

	dir := filepath.Join(editTempDir(), randID(4))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	local := filepath.Join(dir, vfs.Base(f.Caps().PathStyle, remote))
	r, err := f.Open(ctx, remote, 0)
	if err != nil {
		return nil, err
	}
	out, err := os.OpenFile(local, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		r.Close()
		return nil, err
	}
	_, err = io.Copy(out, r)
	r.Close()
	out.Close()
	if err != nil {
		return nil, err
	}
	if e, err := f.Stat(ctx, remote); err == nil {
		os.Chtimes(local, time.Now(), e.ModTime)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Watch the directory: many editors save by renaming a temp file.
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, err
	}
	wctx, cancel := context.WithCancel(context.Background())
	ed := &ExternalEdit{ID: randID(5), Conn: conn, Remote: remote, Local: local, Editor: editor, Opened: time.Now(), w: w, cancel: cancel}
	c.edits.mu.Lock()
	c.edits.m[ed.ID] = ed
	c.edits.mu.Unlock()
	if err := launchEditor(editor, local); err != nil {
		c.StopExternalEdit(ed.ID)
		return nil, fmt.Errorf("cannot start the editor: %w", err)
	}
	c.Bus.Publish("edit.update", ed)
	go c.watchEdit(wctx, ed)
	return ed, nil
}

func (c *Core) watchEdit(ctx context.Context, ed *ExternalEdit) {
	defer ed.w.Close()
	var timer *time.Timer
	fire := make(chan struct{}, 1)
	lastMod := time.Time{}
	if st, err := os.Stat(ed.Local); err == nil {
		lastMod = st.ModTime()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ed.w.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) != filepath.Clean(ed.Local) {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(500*time.Millisecond, func() {
				select {
				case fire <- struct{}{}:
				default:
				}
			})
		case <-ed.w.Errors:
		case <-fire:
			st, err := os.Stat(ed.Local)
			if err != nil || !st.ModTime().After(lastMod) {
				continue
			}
			lastMod = st.ModTime()
			err = c.uploadEdit(ctx, ed)
			c.edits.mu.Lock()
			if err != nil {
				ed.Error = err.Error()
			} else {
				ed.Error = ""
				ed.Uploads++
				ed.Uploaded = time.Now()
			}
			snap := *ed
			c.edits.mu.Unlock()
			c.Bus.Publish("edit.update", snap)
		}
	}
}

func (c *Core) uploadEdit(ctx context.Context, ed *ExternalEdit) error {
	f, err := c.FSFor(ctx, ed.Conn)
	if err != nil {
		return err
	}
	in, err := os.Open(ed.Local)
	if err != nil {
		return err
	}
	defer in.Close()
	st, _ := in.Stat()
	var mode uint32
	if e, err := f.Stat(ctx, ed.Remote); err == nil {
		mode = e.Mode
	}
	w, err := f.Create(ctx, ed.Remote, vfs.CreateOpts{Size: st.Size()})
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if mode != 0 && f.Caps().Chmod {
		f.Chmod(ctx, ed.Remote, mode)
	}
	c.Changed(ed.Conn, vfs.Dir(f.Caps().PathStyle, ed.Remote))
	return nil
}

func (c *Core) StopExternalEdit(id string) {
	c.edits.mu.Lock()
	ed, ok := c.edits.m[id]
	delete(c.edits.m, id)
	c.edits.mu.Unlock()
	if !ok {
		return
	}
	ed.cancel()
	os.RemoveAll(filepath.Dir(ed.Local))
	c.Bus.Publish("edit.closed", id)
}

func (c *Core) ExternalEdits() []ExternalEdit {
	c.edits.mu.Lock()
	defer c.edits.mu.Unlock()
	out := make([]ExternalEdit, 0, len(c.edits.m))
	for _, e := range c.edits.m {
		out = append(out, *e)
	}
	return out
}

func (ee *externalEdits) closeConn(conn string) {
	ee.mu.Lock()
	var ids []string
	for id, e := range ee.m {
		if e.Conn == conn {
			ids = append(ids, id)
		}
	}
	ee.mu.Unlock()
	for _, id := range ids {
		ee.core.StopExternalEdit(id)
	}
}

// OpenRemote downloads a remote file to a temp location and opens it with
// the default application, watching it for changes like WinSCP's "Open".
func (c *Core) OpenRemote(ctx context.Context, conn, remote string) error {
	if conn == LocalID {
		if c.Local.Root != "" {
			return errors.New("not available in web mode")
		}
		return OpenWithDefault(remote)
	}
	_, err := c.EditExternal(ctx, conn, remote, "")
	return err
}
