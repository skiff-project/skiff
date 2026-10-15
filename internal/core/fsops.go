package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"skiff/internal/mask"
	"skiff/internal/vfs"
	"skiff/internal/vfs/localfs"
)

// ListResult is returned to panels.
type ListResult struct {
	Path    string      `json:"path"`
	Entries []vfs.Entry `json:"entries"`
}

func (c *Core) List(ctx context.Context, conn, dir string) (ListResult, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return ListResult{}, err
	}
	if dir == "" {
		dir, _ = f.Home(ctx)
	}
	if conn == LocalID && c.Local.Root == "" {
		dir = localfs.ExpandHome(dir)
	}
	entries, err := f.List(ctx, dir)
	if err != nil && conn != LocalID && isConnErr(err) {
		if cn, cerr := c.Conn(conn); cerr == nil {
			if rerr := cn.Reconnect(ctx); rerr == nil {
				f, _ = cn.FS(ctx)
				entries, err = f.List(ctx, dir)
			}
		}
	}
	if err != nil {
		return ListResult{Path: dir}, err
	}
	if cn, cerr := c.Conn(conn); cerr == nil && cn.site.TimeOffset != 0 {
		off := time.Duration(cn.site.TimeOffset) * time.Minute
		for i := range entries {
			if !entries[i].ModTime.IsZero() {
				entries[i].ModTime = entries[i].ModTime.Add(off)
			}
		}
	}
	if entries == nil {
		entries = []vfs.Entry{}
	}
	return ListResult{Path: dir, Entries: entries}, nil
}

func isConnErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection lost") || strings.Contains(s, "eof") || strings.Contains(s, "closed network connection") ||
		strings.Contains(s, "broken pipe") || strings.Contains(s, "not connected") || strings.Contains(s, "connection reset")
}

func (c *Core) Stat(ctx context.Context, conn, p string) (vfs.Entry, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return vfs.Entry{}, err
	}
	return f.Stat(ctx, p)
}

func (c *Core) Mkdir(ctx context.Context, conn, p string, mode uint32) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	if err := f.Mkdir(ctx, p); err != nil {
		return err
	}
	if mode != 0 && f.Caps().Chmod {
		f.Chmod(ctx, p, mode)
	}
	c.Changed(conn, vfs.Dir(f.Caps().PathStyle, p))
	return nil
}

func (c *Core) CreateFile(ctx context.Context, conn, p string, content []byte) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	if _, err := f.Stat(ctx, p); err == nil {
		return vfs.WrapErr("create", p, vfs.ErrExist)
	}
	w, err := f.Create(ctx, p, vfs.CreateOpts{Size: int64(len(content))})
	if err != nil {
		return err
	}
	if _, err := w.Write(content); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	c.Changed(conn, vfs.Dir(f.Caps().PathStyle, p))
	return nil
}

func (c *Core) Rename(ctx context.Context, conn, from, to string) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	if _, err := f.Stat(ctx, to); err == nil && !strings.EqualFold(from, to) {
		return vfs.WrapErr("rename", to, vfs.ErrExist)
	}
	if err := f.Rename(ctx, from, to); err != nil {
		return err
	}
	style := f.Caps().PathStyle
	c.Changed(conn, vfs.Dir(style, from))
	if vfs.Dir(style, to) != vfs.Dir(style, from) {
		c.Changed(conn, vfs.Dir(style, to))
	}
	return nil
}

// MoveTo moves items into another directory of the same connection.
func (c *Core) MoveTo(ctx context.Context, conn string, paths []string, dstDir string) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	style := f.Caps().PathStyle
	var errs []string
	for _, p := range paths {
		to := vfs.Join(style, dstDir, vfs.Base(style, p))
		if to == p {
			continue
		}
		if strings.HasPrefix(dstDir+"/", p+"/") {
			errs = append(errs, fmt.Sprintf("%s: cannot move a directory into itself", p))
			continue
		}
		if err := f.Rename(ctx, p, to); err != nil {
			errs = append(errs, err.Error())
		}
		c.Changed(conn, vfs.Dir(style, p))
	}
	c.Changed(conn, dstDir)
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// Duplicate copies items on the same connection (server-side when possible).
func (c *Core) Duplicate(ctx context.Context, conn string, paths []string, dstDir string, names map[string]string) (string, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return "", err
	}
	style := f.Caps().PathStyle
	if sc, ok := f.(vfs.ServerCopier); ok && f.Caps().ServerCopy {
		var errs []string
		for _, p := range paths {
			name := names[p]
			if name == "" {
				name = vfs.Base(style, p)
			}
			to := vfs.Join(style, dstDir, name)
			if to == p {
				to = uniqueCopyName(ctx, f, style, to)
			}
			if err := sc.Copy(ctx, p, to); err != nil {
				errs = append(errs, err.Error())
			}
		}
		c.Changed(conn, dstDir)
		if len(errs) > 0 {
			return "", errors.New(strings.Join(errs, "\n"))
		}
		return "", nil
	}
	// Stream through Skiff with the transfer engine.
	prefs := c.Store.Prefs()
	opts := c.DefaultTransferOptions(conn, conn)
	opts.Overwrite = "ask"
	_ = prefs
	id := c.Transfers.Enqueue(transferRequest(conn, conn, paths, dstDir, names, opts))
	return id, nil
}

func uniqueCopyName(ctx context.Context, f vfs.FS, style, p string) string {
	dir, base := vfs.Dir(style, p), vfs.Base(style, p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		suffix := " - copy"
		if i > 1 {
			suffix = fmt.Sprintf(" - copy (%d)", i)
		}
		cand := vfs.Join(style, dir, stem+suffix+ext)
		if _, err := f.Stat(ctx, cand); vfs.IsNotExist(err) {
			return cand
		}
	}
	return p
}

// Delete removes items. useRecycle moves remote items into the session's
// recycle bin directory and local items into the OS trash.
func (c *Core) Delete(ctx context.Context, conn string, paths []string, useRecycle bool) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	style := f.Caps().PathStyle
	var recycle string
	if useRecycle && conn != LocalID {
		if cn, err := c.Conn(conn); err == nil {
			recycle = cn.site.RecycleBin
		}
	}
	var errs []string
	dirs := map[string]bool{}
	for _, p := range paths {
		e, err := f.Stat(ctx, p)
		if err != nil {
			if vfs.IsNotExist(err) {
				continue
			}
			errs = append(errs, err.Error())
			continue
		}
		dirs[vfs.Dir(style, p)] = true
		switch {
		case conn == LocalID && useRecycle:
			if err := moveToTrash(p); err != nil {
				errs = append(errs, err.Error())
			}
		case recycle != "" && !strings.HasPrefix(p, strings.TrimRight(recycle, "/")+"/"):
			vfs.MkdirAll(ctx, f, recycle)
			target := vfs.Join(style, recycle, vfs.Base(style, p))
			if _, err := f.Stat(ctx, target); err == nil {
				ext := path.Ext(target)
				target = strings.TrimSuffix(target, ext) + "-" + time.Now().Format("20060102-150405") + ext
			}
			if err := f.Rename(ctx, p, target); err != nil {
				errs = append(errs, err.Error())
			}
		default:
			if err := vfs.RemoveAll(ctx, f, p, e.IsDir && !e.IsLink); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	for d := range dirs {
		c.Changed(conn, d)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// Attrs for SetAttrs; nil fields are left unchanged.
type Attrs struct {
	Mode       *uint32    `json:"mode,omitempty"`
	AddXToDirs bool       `json:"addXToDirs,omitempty"`
	Owner      string     `json:"owner,omitempty"`
	Group      string     `json:"group,omitempty"`
	MTime      *time.Time `json:"mtime,omitempty"`
	Recursive  bool       `json:"recursive,omitempty"`
}

func (c *Core) SetAttrs(ctx context.Context, conn string, paths []string, a Attrs) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	var errs []string
	apply := func(p string, isDir bool) {
		if a.Mode != nil {
			m := *a.Mode
			if isDir && a.AddXToDirs {
				m |= (m & 0o444) >> 2
			}
			if err := f.Chmod(ctx, p, m); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if a.Owner != "" || a.Group != "" {
			if ch, ok := f.(vfs.Chowner); ok {
				if err := ch.Chown(ctx, p, a.Owner, a.Group); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
		if a.MTime != nil {
			if err := f.Chtimes(ctx, p, *a.MTime); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	style := f.Caps().PathStyle
	for _, p := range paths {
		e, err := f.Stat(ctx, p)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		isDir := e.IsDir && !e.IsLink
		apply(p, isDir)
		if isDir && a.Recursive {
			vfs.Walk(ctx, f, p, func(cp string, ce vfs.Entry, err error) error {
				if err != nil {
					errs = append(errs, err.Error())
					return nil
				}
				if ce.IsLink {
					return nil
				}
				apply(cp, ce.IsDir)
				return nil
			})
		}
		c.Changed(conn, vfs.Dir(style, p))
	}
	if len(errs) > 0 {
		if len(errs) > 10 {
			errs = append(errs[:10], fmt.Sprintf("… and %d more errors", len(errs)-10))
		}
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

func (c *Core) Link(ctx context.Context, conn, target, link string, hard bool) error {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return err
	}
	l, ok := f.(vfs.Linker)
	if !ok {
		return vfs.ErrUnsupported
	}
	if err := l.Symlink(ctx, target, link, hard); err != nil {
		return err
	}
	c.Changed(conn, vfs.Dir(f.Caps().PathStyle, link))
	return nil
}

type SizeResult struct {
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
	Dirs  int   `json:"dirs"`
}

func (c *Core) CalcSize(ctx context.Context, conn string, paths []string) (SizeResult, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return SizeResult{}, err
	}
	var r SizeResult
	for _, p := range paths {
		e, err := f.Stat(ctx, p)
		if err != nil {
			return r, err
		}
		if !e.IsDir || e.IsLink {
			r.Bytes += e.Size
			r.Files++
			continue
		}
		r.Dirs++
		err = vfs.Walk(ctx, f, p, func(_ string, ce vfs.Entry, err error) error {
			if err != nil {
				return nil
			}
			if ce.IsDir && !ce.IsLink {
				r.Dirs++
			} else {
				r.Files++
				r.Bytes += ce.Size
			}
			return nil
		})
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

func (c *Core) Checksum(ctx context.Context, conn, p, algo string) (string, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return "", err
	}
	cs, ok := f.(vfs.Checksummer)
	if !ok || !f.Caps().Checksum {
		return "", vfs.ErrUnsupported
	}
	return cs.Checksum(ctx, p, algo)
}

type SpaceResult struct {
	Total int64 `json:"total"`
	Free  int64 `json:"free"`
}

func (c *Core) Space(ctx context.Context, conn, p string) (SpaceResult, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return SpaceResult{}, err
	}
	sr, ok := f.(vfs.SpaceReporter)
	if !ok {
		return SpaceResult{}, vfs.ErrUnsupported
	}
	t, fr, err := sr.Space(ctx, p)
	return SpaceResult{Total: t, Free: fr}, err
}

type ExecResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

// Exec runs a shell command (in cwd) on the connection.
func (c *Core) Exec(ctx context.Context, conn, command, cwd string) (ExecResult, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return ExecResult{}, err
	}
	ex, ok := f.(vfs.Executor)
	if !ok || !f.Caps().Exec {
		return ExecResult{}, errors.New("this protocol cannot execute commands")
	}
	cmd := command
	if cwd != "" && f.Caps().Protocol != "ftp" {
		if f.Caps().PathStyle == "windows" {
			cmd = "cd /d \"" + cwd + "\" && " + command
		} else {
			cmd = "cd " + vfs.ShellQuote(cwd) + " && " + command
		}
	}
	var out, errb limitedBuffer
	out.max, errb.max = 4<<20, 1<<20
	code, err := ex.Exec(ctx, cmd, nil, &out, &errb)
	if err != nil {
		return ExecResult{}, err
	}
	if cwd != "" {
		c.Changed(conn, cwd)
	}
	return ExecResult{Stdout: out.String(), Stderr: errb.String(), Code: code}, nil
}

type limitedBuffer struct {
	bytes.Buffer
	max       int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.Len()+len(p) > l.max {
		l.truncated = true
		room := l.max - l.Len()
		if room > 0 {
			l.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return l.Buffer.Write(p)
}

func (l *limitedBuffer) String() string {
	s := l.Buffer.String()
	if l.truncated {
		s += "\n… (output truncated)"
	}
	return s
}

func (c *Core) ServerInfo(conn string) ([]vfs.InfoItem, error) {
	cn, err := c.Conn(conn)
	if err != nil {
		return nil, err
	}
	cn.mu.Lock()
	defer cn.mu.Unlock()
	out := append([]vfs.InfoItem(nil), cn.info...)
	out = append(out, vfs.InfoItem{Group: "Session", Key: "Connected since", Value: cn.created.Format(time.RFC3339)})
	return out, nil
}

// ---- find ----

type FindOptions struct {
	Mask       string `json:"mask"`
	Contains   string `json:"contains,omitempty"`
	MinSize    int64  `json:"minSize,omitempty"`
	MaxSize    int64  `json:"maxSize,omitempty"`
	NewerThan  *time.Time `json:"newerThan,omitempty"`
	OlderThan  *time.Time `json:"olderThan,omitempty"`
	IncludeDirs bool  `json:"includeDirs,omitempty"`
	MaxResults int    `json:"maxResults,omitempty"`
}

type FindHit struct {
	Path  string    `json:"path"`
	Entry vfs.Entry `json:"entry"`
}

// Find searches recursively and streams hits as "find.hits" events.
func (c *Core) Find(conn, dir string, o FindOptions) (string, error) {
	f, err := c.FSFor(context.Background(), conn)
	if err != nil {
		return "", err
	}
	m, err := mask.Parse(o.Mask)
	if err != nil {
		return "", err
	}
	id := randID(6)
	ctx, cancel := context.WithCancel(context.Background())
	c.finds.Store(id, cancel)
	if o.MaxResults <= 0 {
		o.MaxResults = 5000
	}
	go func() {
		defer c.finds.Delete(id)
		defer cancel()
		var batch []FindHit
		var mu sync.Mutex
		count, scanned := 0, 0
		flush := func(final bool, errMsg string) {
			mu.Lock()
			b := batch
			batch = nil
			mu.Unlock()
			c.Bus.Publish("find.hits", map[string]any{"id": id, "hits": b, "done": final, "scanned": scanned, "error": errMsg})
		}
		last := time.Now()
		var grepper vfs.Executor
		if o.Contains != "" {
			if ex, ok := f.(vfs.Executor); ok && f.Caps().Exec && f.Caps().Protocol != "ftp" {
				grepper = ex
			}
		}
		err := vfs.Walk(ctx, f, dir, func(p string, e vfs.Entry, err error) error {
			if err != nil {
				return nil
			}
			scanned++
			rel := vfs.Rel(f.Caps().PathStyle, dir, p)
			if e.IsDir && !o.IncludeDirs {
				return nil
			}
			if !m.Match(e.Name, rel, e.IsDir, e.Size, e.ModTime) {
				return nil
			}
			if o.MinSize > 0 && e.Size < o.MinSize || o.MaxSize > 0 && e.Size > o.MaxSize {
				return nil
			}
			if o.NewerThan != nil && e.ModTime.Before(*o.NewerThan) || o.OlderThan != nil && e.ModTime.After(*o.OlderThan) {
				return nil
			}
			if o.Contains != "" && !e.IsDir {
				if !c.fileContains(ctx, f, grepper, p, e.Size, o.Contains) {
					return nil
				}
			}
			mu.Lock()
			batch = append(batch, FindHit{Path: p, Entry: e})
			mu.Unlock()
			count++
			if count >= o.MaxResults {
				return errors.New("too many results")
			}
			if time.Since(last) > 250*time.Millisecond {
				flush(false, "")
				last = time.Now()
			}
			return nil
		})
		msg := ""
		if err != nil && !errors.Is(err, context.Canceled) {
			msg = err.Error()
		}
		flush(true, msg)
	}()
	return id, nil
}

func (c *Core) fileContains(ctx context.Context, f vfs.FS, ex vfs.Executor, p string, size int64, needle string) bool {
	if ex != nil {
		code, err := ex.Exec(ctx, "grep -qF -- "+vfs.ShellQuote(needle)+" "+vfs.ShellQuote(p), nil, io.Discard, io.Discard)
		return err == nil && code == 0
	}
	if size > 50<<20 {
		return false
	}
	r, err := f.Open(ctx, p, 0)
	if err != nil {
		return false
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 50<<20))
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(data), bytes.ToLower([]byte(needle)))
}

func (c *Core) CancelFind(id string) {
	if v, ok := c.finds.Load(id); ok {
		v.(context.CancelFunc)()
	}
}

// ---- editor I/O ----

type FileContent struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	Encoding string    `json:"encoding"`
	BOM      bool      `json:"bom"`
	EOL      string    `json:"eol"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime"`
	Binary   bool      `json:"binary"`
	ReadOnly bool      `json:"readOnly"`
}

var encodings = map[string]encoding.Encoding{
	"windows-1252": charmap.Windows1252,
	"iso-8859-1":   charmap.ISO8859_1,
	"iso-8859-15":  charmap.ISO8859_15,
	"windows-1250": charmap.Windows1250,
	"windows-1251": charmap.Windows1251,
	"koi8-r":       charmap.KOI8R,
	"cp850":        charmap.CodePage850,
	"cp437":        charmap.CodePage437,
	"utf-16le":     unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
	"utf-16be":     unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM),
}

// ReadText loads a text file for the internal editor.
func (c *Core) ReadText(ctx context.Context, conn, p, forceEncoding string) (FileContent, error) {
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return FileContent{}, err
	}
	e, err := f.Stat(ctx, p)
	if err != nil {
		return FileContent{}, err
	}
	limit := int64(c.Store.Prefs().Editor.MaxSizeMB) << 20
	if e.Size > limit {
		return FileContent{}, fmt.Errorf("file is too large for the internal editor (%d MB limit)", limit>>20)
	}
	r, err := f.Open(ctx, p, 0)
	if err != nil {
		return FileContent{}, err
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	r.Close()
	if err != nil {
		return FileContent{}, err
	}
	fc := FileContent{Path: p, Size: int64(len(data)), MTime: e.ModTime}
	enc := strings.ToLower(forceEncoding)
	switch {
	case enc != "" && enc != "auto":
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		enc, fc.BOM = "utf-8", true
		data = data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		enc, fc.BOM = "utf-16le", true
		data = data[2:]
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		enc, fc.BOM = "utf-16be", true
		data = data[2:]
	default:
		sample := data
		if len(sample) > 8192 {
			sample = sample[:8192]
		}
		if bytes.IndexByte(sample, 0) >= 0 {
			fc.Binary = true
		}
		if utf8.Valid(data) {
			enc = "utf-8"
		} else {
			enc = "windows-1252"
		}
	}
	fc.Encoding = enc
	text := string(data)
	if e, ok := encodings[enc]; ok {
		if s, err := e.NewDecoder().String(text); err == nil {
			text = s
		}
	}
	crlf := strings.Count(text, "\r\n")
	lf := strings.Count(text, "\n") - crlf
	fc.EOL = "LF"
	if crlf > lf {
		fc.EOL = "CRLF"
	} else if crlf == 0 && lf == 0 && strings.Contains(text, "\r") {
		fc.EOL = "CR"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if fc.EOL == "CR" {
		text = strings.ReplaceAll(text, "\r", "\n")
	}
	fc.Content = text
	return fc, nil
}

type WriteText struct {
	Conn     string     `json:"conn"`
	Path     string     `json:"path"`
	Content  string     `json:"content"`
	Encoding string     `json:"encoding"`
	BOM      bool       `json:"bom"`
	EOL      string     `json:"eol"`
	Expect   *time.Time `json:"expectMtime,omitempty"`
	Force    bool       `json:"force,omitempty"`
}

var ErrConflict = errors.New("the file was modified on the server since it was opened")

// WriteText saves editor content, refusing when the file changed meanwhile.
func (c *Core) WriteText(ctx context.Context, w WriteText) (vfs.Entry, error) {
	f, err := c.FSFor(ctx, w.Conn)
	if err != nil {
		return vfs.Entry{}, err
	}
	if w.Expect != nil && !w.Force {
		if e, err := f.Stat(ctx, w.Path); err == nil && !e.ModTime.IsZero() && e.ModTime.Sub(*w.Expect).Abs() > time.Second {
			return e, ErrConflict
		}
	}
	text := w.Content
	switch w.EOL {
	case "CRLF":
		text = strings.ReplaceAll(text, "\n", "\r\n")
	case "CR":
		text = strings.ReplaceAll(text, "\n", "\r")
	}
	data := []byte(text)
	if e, ok := encodings[strings.ToLower(w.Encoding)]; ok {
		if b, err := e.NewEncoder().Bytes(data); err == nil {
			data = b
		} else {
			return vfs.Entry{}, fmt.Errorf("cannot encode as %s: %w", w.Encoding, err)
		}
	}
	if w.BOM {
		switch strings.ToLower(w.Encoding) {
		case "utf-8":
			data = append([]byte{0xEF, 0xBB, 0xBF}, data...)
		case "utf-16le":
			data = append([]byte{0xFF, 0xFE}, data...)
		case "utf-16be":
			data = append([]byte{0xFE, 0xFF}, data...)
		}
	}
	var mode uint32
	if e, err := f.Stat(ctx, w.Path); err == nil {
		mode = e.Mode
	}
	wc, err := f.Create(ctx, w.Path, vfs.CreateOpts{Size: int64(len(data))})
	if err != nil {
		return vfs.Entry{}, err
	}
	if _, err := wc.Write(data); err != nil {
		wc.Close()
		return vfs.Entry{}, err
	}
	if err := wc.Close(); err != nil {
		return vfs.Entry{}, err
	}
	if mode != 0 && f.Caps().Chmod {
		f.Chmod(ctx, w.Path, mode)
	}
	c.Changed(w.Conn, vfs.Dir(f.Caps().PathStyle, w.Path))
	return f.Stat(ctx, w.Path)
}
