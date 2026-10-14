package ftpfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"skiff/internal/netx"
	"skiff/internal/vfs"
	"skiff/internal/vfs/scpfs"
)

type FS struct {
	cfg       Config
	c         *client
	keepalive time.Duration
	stop      chan struct{}
}

// New connects and logs in.
func New(ctx context.Context, cfg Config, keepalive time.Duration) (*FS, error) {
	c, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	f := &FS{cfg: cfg, c: c, keepalive: keepalive, stop: make(chan struct{})}
	if keepalive > 0 {
		go f.keepaliveLoop()
	}
	return f, nil
}

func (f *FS) keepaliveLoop() {
	t := time.NewTicker(f.keepalive / 2)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			if !f.c.mu.TryLock() {
				continue
			}
			if !f.c.closed && time.Since(f.c.lastUsed) >= f.keepalive {
				f.c.cmd("NOOP")
			}
			f.c.mu.Unlock()
		}
	}
}

func isNetErr(err error) bool {
	if err == nil {
		return false
	}
	var pe *ProtoError
	if errors.As(err, &pe) {
		return pe.Code == 421
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "connection reset")
}

// do runs fn holding the control connection, reconnecting once on a broken
// connection when retry is true.
func (f *FS) do(ctx context.Context, retry bool, fn func(c *client) error) error {
	f.c.mu.Lock()
	defer f.c.mu.Unlock()
	if f.c.closed {
		if err := f.reconnect(ctx); err != nil {
			return err
		}
	}
	err := fn(f.c)
	if err != nil && retry && isNetErr(err) && ctx.Err() == nil {
		f.c.logf("Connection lost (%v), reconnecting", err)
		if rerr := f.reconnect(ctx); rerr != nil {
			return err
		}
		err = fn(f.c)
	}
	return err
}

func (f *FS) reconnect(ctx context.Context) error {
	f.c.conn.Close()
	nc := &client{cfg: f.c.cfg, feat: map[string]string{}, enc: f.c.enc}
	if err := nc.connect(ctx); err != nil {
		f.c.closed = true
		return err
	}
	f.c.conn, f.c.text, f.c.feat, f.c.prot, f.c.pwd, f.c.closed, f.c.lastUsed = nc.conn, nc.text, nc.feat, nc.prot, nc.pwd, false, time.Now()
	return nil
}

func (f *FS) mlsd() bool {
	switch f.cfg.UseMLSD {
	case "on":
		return true
	case "off":
		return false
	}
	return f.c.has("MLST") || f.c.has("MLSD")
}

func (f *FS) Caps() vfs.Caps {
	prec := time.Minute
	if f.mlsd() || f.c.has("MDTM") {
		prec = time.Second
	}
	return vfs.Caps{
		Protocol:       "ftp",
		PathStyle:      "posix",
		Permissions:    true,
		Chmod:          true,
		Owners:         true,
		SetMtime:       f.c.has("MFMT") || f.c.has("MDTM"),
		Exec:           true,
		Checksum:       f.c.has("HASH") || f.c.has("XSHA256") || f.c.has("XSHA1") || f.c.has("XMD5"),
		ResumeDownload: f.c.has("REST"),
		ResumeUpload:   true,
		MtimePrecision: prec,
	}
}

func (f *FS) Home(ctx context.Context) (string, error) { return f.c.pwd, nil }

func (f *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	var out []vfs.Entry
	err := f.do(ctx, true, func(c *client) error {
		var err error
		out, err = f.list(ctx, c, dir)
		return err
	})
	return out, vfs.WrapErr("list", dir, err)
}

func (f *FS) list(ctx context.Context, c *client, dir string) ([]vfs.Entry, error) {
	ed := c.encode(dir)
	if f.mlsd() {
		lines, err := c.readLines(ctx, "MLSD "+ed)
		if err == nil {
			var out []vfs.Entry
			for _, ln := range lines {
				if e, ok := parseMLSx(c.decode(ln)); ok {
					out = append(out, e)
				}
			}
			return out, nil
		}
		var pe *ProtoError
		if !errors.As(err, &pe) || pe.Code == 550 || pe.Code == 450 {
			return nil, mapErr(err)
		}
		f.cfg.UseMLSD = "off"
	}
	// LIST does not accept paths with spaces on some servers: CWD first.
	if _, err := c.expect("CWD "+ed, 250); err != nil {
		return nil, mapErr(err)
	}
	cmd := c.listCmd
	if cmd == "" {
		cmd = "LIST"
		if f.cfg.ListHidden {
			cmd = "LIST -a"
		}
	}
	lines, err := c.readLines(ctx, cmd)
	if err != nil && cmd == "LIST -a" {
		var pe *ProtoError
		if errors.As(err, &pe) {
			cmd = "LIST"
			lines, err = c.readLines(ctx, cmd)
		}
	}
	if err != nil {
		return nil, mapErr(err)
	}
	c.listCmd = cmd
	var out []vfs.Entry
	for _, ln := range lines {
		ln = c.decode(ln)
		if e, ok := scpfs.ParseLsLine(ln, ""); ok {
			if e.Name == "." || e.Name == ".." {
				continue
			}
			out = append(out, e)
		} else if e, ok := parseDOS(ln); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func mapErr(err error) error {
	var pe *ProtoError
	if errors.As(err, &pe) {
		low := strings.ToLower(pe.Msg)
		switch {
		case pe.Code == 550 && (strings.Contains(low, "no such") || strings.Contains(low, "not found") || strings.Contains(low, "not exist") || strings.Contains(low, "can't check for file existence") || strings.Contains(low, "failed to")):
			return fmt.Errorf("%s: %w", pe.Msg, vfs.ErrNotExist)
		case strings.Contains(low, "permission denied") || pe.Code == 553:
			return fmt.Errorf("%s: %w", pe.Msg, vfs.ErrPermission)
		case strings.Contains(low, "exists"):
			return fmt.Errorf("%s: %w", pe.Msg, vfs.ErrExist)
		}
	}
	return err
}

// parseMLSx parses an MLSD/MLST fact line.
func parseMLSx(ln string) (vfs.Entry, bool) {
	ln = strings.TrimLeft(ln, " ")
	i := strings.Index(ln, " ")
	if i < 0 {
		return vfs.Entry{}, false
	}
	facts, name := ln[:i], ln[i+1:]
	e := vfs.Entry{Name: name, Hidden: strings.HasPrefix(name, ".")}
	var typ string
	for _, fact := range strings.Split(facts, ";") {
		kv := strings.SplitN(fact, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k, v := strings.ToLower(kv[0]), kv[1]
		switch k {
		case "type":
			typ = strings.ToLower(v)
		case "size", "sizd":
			e.Size, _ = strconv.ParseInt(v, 10, 64)
		case "modify":
			e.ModTime = parseFTPTime(v)
		case "unix.mode":
			m, _ := strconv.ParseUint(v, 8, 32)
			e.Mode = uint32(m) & 0o7777
		case "unix.owner", "unix.ownername":
			e.Owner = v
		case "unix.group", "unix.groupname":
			e.Group = v
		case "unix.uid":
			e.UID, _ = strconv.Atoi(v)
			if e.Owner == "" {
				e.Owner = v
			}
		case "unix.gid":
			e.GID, _ = strconv.Atoi(v)
			if e.Group == "" {
				e.Group = v
			}
		case "perm":
			if e.Mode == 0 {
				e.Mode = permFromFacts(v)
			}
		}
	}
	switch {
	case typ == "cdir" || typ == "pdir":
		return vfs.Entry{}, false
	case typ == "dir":
		e.IsDir = true
		e.Size = 0
	case strings.HasPrefix(typ, "os.unix=slink") || strings.HasPrefix(typ, "os.unix=symlink"):
		e.IsLink = true
		if j := strings.Index(typ, ":"); j >= 0 {
			e.LinkTarget = facts[strings.Index(strings.ToLower(facts), "slink:")+6:]
			if k := strings.Index(e.LinkTarget, ";"); k >= 0 {
				e.LinkTarget = e.LinkTarget[:k]
			}
		}
	}
	return e, name != "" && name != "." && name != ".."
}

func permFromFacts(p string) uint32 {
	var m uint32 = 0o444
	if strings.ContainsAny(p, "wacm") {
		m |= 0o200
	}
	if strings.ContainsAny(p, "el") {
		m |= 0o111
	}
	return m
}

func parseFTPTime(v string) time.Time {
	v = strings.TrimSpace(v)
	layouts := []string{"20060102150405.999", "20060102150405"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, v, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseDOS parses IIS/DOS style listings:
//   01-02-24  03:04PM       <DIR>          name
//   01-02-2024  15:04            12345 name
func parseDOS(ln string) (vfs.Entry, bool) {
	fs := strings.Fields(ln)
	if len(fs) < 4 {
		return vfs.Entry{}, false
	}
	var t time.Time
	var err error
	for _, l := range []string{"01-02-06 03:04PM", "01-02-2006 03:04PM", "01-02-06 15:04", "01-02-2006 15:04", "2006-01-02 15:04"} {
		if t, err = time.ParseInLocation(l, fs[0]+" "+fs[1], time.Local); err == nil {
			break
		}
	}
	if err != nil {
		return vfs.Entry{}, false
	}
	e := vfs.Entry{ModTime: t, Mode: 0o644}
	if strings.EqualFold(fs[2], "<DIR>") {
		e.IsDir = true
		e.Mode = 0o755
	} else {
		e.Size, err = strconv.ParseInt(strings.ReplaceAll(fs[2], ",", ""), 10, 64)
		if err != nil {
			return vfs.Entry{}, false
		}
	}
	// name is the rest after the third field
	idx := 0
	for n := 0; n < 3; n++ {
		idx += strings.Index(ln[idx:], fs[n]) + len(fs[n])
	}
	e.Name = strings.TrimLeft(ln[idx:], " ")
	return e, e.Name != ""
}

func (f *FS) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	if p == "/" || p == "" {
		return vfs.Entry{Name: "/", IsDir: true}, nil
	}
	var e vfs.Entry
	err := f.do(ctx, true, func(c *client) error {
		if c.has("MLST") && f.cfg.UseMLSD != "off" {
			r, err := c.cmd("MLST " + c.encode(p))
			if err != nil {
				return err
			}
			if r.Code == 250 {
				for _, ln := range strings.Split(r.Msg, "\n") {
					if !strings.HasPrefix(ln, " ") || !strings.Contains(ln, "=") {
						continue
					}
					low := strings.ToLower(ln)
					pe, _ := parseMLSx(c.decode(ln))
					pe.Name = path.Base(p)
					if strings.Contains(low, "type=dir") || strings.Contains(low, "type=cdir") || strings.Contains(low, "type=pdir") {
						pe.IsDir = true
						pe.Size = 0
					}
					e = pe
					return nil
				}
			}
			if r.Code == 550 {
				return mapErr(&ProtoError{r})
			}
		}
		// Fall back to listing the parent directory.
		entries, err := f.list(ctx, c, path.Dir(p))
		if err != nil {
			return err
		}
		base := path.Base(p)
		for _, x := range entries {
			if x.Name == base {
				e = x
				return nil
			}
		}
		return vfs.ErrNotExist
	})
	if err != nil {
		return vfs.Entry{}, vfs.WrapErr("stat", p, err)
	}
	return e, nil
}

func (f *FS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	f.c.mu.Lock()
	if f.c.closed {
		if err := f.reconnect(ctx); err != nil {
			f.c.mu.Unlock()
			return nil, err
		}
	}
	dc, err := f.c.openData(ctx, "RETR "+f.c.encode(p), offset)
	if err != nil && isNetErr(err) {
		if f.reconnect(ctx) == nil {
			dc, err = f.c.openData(ctx, "RETR "+f.c.encode(p), offset)
		}
	}
	if err != nil {
		f.c.mu.Unlock()
		return nil, vfs.WrapErr("download", p, mapErr(err))
	}
	return &readStream{dataStream: &dataStream{Conn: dc, c: f.c, unlock: f.c.mu.Unlock}}, nil
}

func (f *FS) Create(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	f.c.mu.Lock()
	if f.c.closed {
		if err := f.reconnect(ctx); err != nil {
			f.c.mu.Unlock()
			return nil, err
		}
	}
	cmd := "STOR "
	var offset int64
	if opt.Offset > 0 {
		if f.c.has("REST") {
			offset = opt.Offset
		} else {
			cmd = "APPE "
		}
	}
	dc, err := f.c.openData(ctx, cmd+f.c.encode(p), offset)
	if err != nil && isNetErr(err) {
		if f.reconnect(ctx) == nil {
			dc, err = f.c.openData(ctx, cmd+f.c.encode(p), offset)
		}
	}
	if err != nil {
		f.c.mu.Unlock()
		return nil, vfs.WrapErr("upload", p, mapErr(err))
	}
	return &dataStream{Conn: dc, c: f.c, unlock: f.c.mu.Unlock}, nil
}

func (f *FS) simple(ctx context.Context, op, p, cmd string, codes ...int) error {
	err := f.do(ctx, true, func(c *client) error {
		_, err := c.expect(cmd, codes...)
		return err
	})
	return vfs.WrapErr(op, p, mapErr(err))
}

func (f *FS) Mkdir(ctx context.Context, p string) error {
	return f.simple(ctx, "mkdir", p, "MKD "+f.c.encode(p), 257, 250)
}

func (f *FS) Remove(ctx context.Context, p string, isDir bool) error {
	if isDir {
		return f.simple(ctx, "rmdir", p, "RMD "+f.c.encode(p), 250)
	}
	return f.simple(ctx, "delete", p, "DELE "+f.c.encode(p), 250, 200)
}

func (f *FS) Rename(ctx context.Context, from, to string) error {
	err := f.do(ctx, true, func(c *client) error {
		if _, err := c.expect("RNFR "+c.encode(from), 350); err != nil {
			return err
		}
		_, err := c.expect("RNTO "+c.encode(to), 250, 200)
		return err
	})
	return vfs.WrapErr("rename", from, mapErr(err))
}

func (f *FS) Chmod(ctx context.Context, p string, mode uint32) error {
	return f.simple(ctx, "chmod", p, fmt.Sprintf("SITE CHMOD %o %s", mode&0o7777, f.c.encode(p)), 200, 250)
}

func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	ts := mtime.UTC().Format("20060102150405")
	err := f.do(ctx, true, func(c *client) error {
		ep := c.encode(p)
		if c.has("MFMT") {
			_, err := c.expect("MFMT "+ts+" "+ep, 213, 250)
			return err
		}
		if r, err := c.cmd("MDTM " + ts + " " + ep); err == nil && (r.Code == 213 || r.Code == 253 || r.Code == 250) {
			return nil
		}
		_, err := c.expect("SITE UTIME "+ts+" "+ep, 200, 250)
		return err
	})
	if err != nil {
		return vfs.WrapErr("touch", p, vfs.ErrUnsupported)
	}
	return nil
}

func (f *FS) Checksum(ctx context.Context, p, algo string) (string, error) {
	algo = strings.ToUpper(strings.ReplaceAll(algo, "-", ""))
	if algo == "" {
		algo = "SHA256"
	}
	var sum string
	err := f.do(ctx, true, func(c *client) error {
		ep := c.encode(p)
		if c.has("HASH") {
			name := map[string]string{"SHA256": "SHA-256", "SHA1": "SHA-1", "MD5": "MD5", "SHA512": "SHA-512"}[algo]
			if name != "" {
				if _, err := c.expect("OPTS HASH "+name, 200); err == nil {
					r, err := c.expect("HASH "+ep, 213)
					if err == nil {
						fs := strings.Fields(r.Msg)
						if len(fs) >= 3 {
							sum = strings.ToLower(fs[2])
							return nil
						}
					}
				}
			}
		}
		cmd := map[string]string{"SHA256": "XSHA256", "SHA1": "XSHA1", "MD5": "XMD5", "SHA512": "XSHA512"}[algo]
		if cmd == "" || !c.has(cmd) {
			return vfs.ErrUnsupported
		}
		r, err := c.expect(cmd+" "+ep, 250, 213)
		if err != nil {
			return err
		}
		fs := strings.Fields(r.Msg)
		if len(fs) == 0 {
			return errors.New("empty checksum reply")
		}
		sum = strings.ToLower(fs[len(fs)-1])
		if len(sum) < 16 && len(fs) > 0 {
			sum = strings.ToLower(fs[0])
		}
		return nil
	})
	return sum, err
}

// Exec sends a raw FTP command (used by "custom commands" / console).
func (f *FS) Exec(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	var r Response
	err := f.do(ctx, false, func(c *client) error {
		var err error
		r, err = c.cmd(c.encode(cmd))
		return err
	})
	if err != nil {
		return -1, err
	}
	fmt.Fprintf(stdout, "%d %s\n", r.Code, r.Msg)
	if isNeg(r.Code) {
		return 1, nil
	}
	return 0, nil
}

func (f *FS) Fork(ctx context.Context) (vfs.FS, error) {
	return New(ctx, f.cfg, f.keepalive)
}

func (f *FS) Info() []vfs.InfoItem {
	out := []vfs.InfoItem{
		{Group: "FTP", Key: "Server", Value: f.c.addr()},
		{Group: "FTP", Key: "System", Value: f.c.system},
		{Group: "FTP", Key: "Welcome", Value: f.c.welcome},
		{Group: "FTP", Key: "Mode", Value: map[bool]string{true: "Passive", false: "Active"}[f.cfg.Passive]},
		{Group: "FTP", Key: "Listing", Value: map[bool]string{true: "MLSD", false: "LIST"}[f.mlsd()]},
	}
	var feats []string
	for k := range f.c.feat {
		feats = append(feats, k)
	}
	sort.Strings(feats)
	out = append(out, vfs.InfoItem{Group: "FTP", Key: "Features", Value: strings.Join(feats, ", ")})
	if f.cfg.TLSVerifier != nil {
		for k, v := range netx.TLSInfo(f.cfg.TLSVerifier.LastState) {
			out = append(out, vfs.InfoItem{Group: "TLS", Key: k, Value: v})
		}
	}
	return out
}

func (f *FS) Close() error {
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
	if !f.c.mu.TryLock() {
		// A transfer still holds the control connection: drop it.
		f.c.closed = true
		f.c.conn.Close()
		return nil
	}
	defer f.c.mu.Unlock()
	f.c.quit()
	return nil
}

