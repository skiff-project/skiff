// Package scpfs implements vfs.FS using the SCP protocol for transfers and
// shell commands for everything else, like WinSCP's SCP mode.
package scpfs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"skiff/internal/sshx"
	"skiff/internal/vfs"
	"skiff/internal/vfs/sftpfs"
)

type Options struct {
	// Shell overrides the remote shell (e.g. "sudo -s").
	Shell string
	// ListCommand overrides directory listing ("ls -la").
	ListCommand string
	Logf        func(string, ...any)
}

type FS struct {
	conn *sshx.Conn
	opts Options
	fork bool
	// command-only mode (no SSH session): see NewExec
	execFn ExecFunc
	exec   *ExecOptions

	mu       sync.Mutex
	listMode string // "find", "lsfull", "lsT", "ls"
	home     string
}

func New(conn *sshx.Conn, opts Options) *FS { return &FS{conn: conn, opts: opts} }

func (f *FS) SSH() *sshx.Conn { return f.conn }

func (f *FS) Caps() vfs.Caps {
	if f.exec != nil {
		return f.execCaps()
	}
	return vfs.Caps{
		Protocol:       "scp",
		PathStyle:      "posix",
		Permissions:    true,
		Chmod:          true,
		Chown:          true,
		Owners:         true,
		Symlink:        true,
		Hardlink:       true,
		SetMtime:       true,
		Exec:           true,
		Shell:          true,
		ServerCopy:     true,
		Checksum:       true,
		Space:          true,
		ResumeDownload: true,
		ResumeUpload:   false,
		Tunnels:        true,
		MtimePrecision: time.Second,
	}
}

func (f *FS) wrap(cmd string) string {
	if f.opts.Shell != "" {
		return f.opts.Shell + " -c " + vfs.ShellQuote(cmd)
	}
	return cmd
}

func (f *FS) Exec(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if f.execFn != nil {
		return f.execFn(ctx, f.wrap(cmd), stdin, stdout, stderr)
	}
	return sftpfs.ExecSSH(ctx, f.conn.Client, f.wrap(cmd), stdin, stdout, stderr)
}

// run executes cmd and returns stdout, failing on a non-zero exit status.
func (f *FS) run(ctx context.Context, cmd string) (string, error) {
	var out, errb bytes.Buffer
	code, err := f.Exec(ctx, cmd, nil, &out, &errb)
	if err != nil {
		return "", err
	}
	if code != 0 {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = fmt.Sprintf("command failed with exit code %d", code)
		}
		low := strings.ToLower(msg)
		switch {
		case strings.Contains(low, "no such file"):
			return out.String(), fmt.Errorf("%s: %w", msg, vfs.ErrNotExist)
		case strings.Contains(low, "permission denied"):
			return out.String(), fmt.Errorf("%s: %w", msg, vfs.ErrPermission)
		case strings.Contains(low, "file exists"):
			return out.String(), fmt.Errorf("%s: %w", msg, vfs.ErrExist)
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

func (f *FS) Home(ctx context.Context) (string, error) {
	if f.home != "" {
		return f.home, nil
	}
	out, err := f.run(ctx, "pwd")
	if err != nil {
		return "/", nil
	}
	f.home = strings.TrimSpace(out)
	if f.home == "" {
		f.home = "/"
	}
	return f.home, nil
}

const findFmt = `%y\t%Y\t%m\t%s\t%T@\t%u\t%g\t%U\t%G\t%l\t%f\0`

func (f *FS) detect(ctx context.Context) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listMode != "" {
		return f.listMode
	}
	if f.opts.ListCommand != "" {
		f.listMode = "custom"
		return f.listMode
	}
	if out, err := f.run(ctx, "find / -maxdepth 0 -printf '%y'"); err == nil && strings.TrimSpace(out) == "d" {
		f.listMode = "find"
	} else if _, err := f.run(ctx, "LC_ALL=C ls -lad --full-time /"); err == nil {
		f.listMode = "lsfull"
	} else if _, err := f.run(ctx, "LC_ALL=C ls -ladT /"); err == nil {
		f.listMode = "lsT"
	} else {
		f.listMode = "ls"
	}
	return f.listMode
}

func (f *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	q := vfs.ShellQuote(dir)
	switch f.detect(ctx) {
	case "find":
		out, err := f.run(ctx, "cd -- "+q+" && find . -mindepth 1 -maxdepth 1 -printf '"+findFmt+"'")
		if err != nil {
			return nil, vfs.WrapErr("list", dir, err)
		}
		return parseFind(out), nil
	case "lsfull":
		out, err := f.run(ctx, "LC_ALL=C ls -la --full-time -- "+q)
		if err != nil {
			return nil, vfs.WrapErr("list", dir, err)
		}
		return parseLs(out, "full"), nil
	case "lsT":
		out, err := f.run(ctx, "LC_ALL=C ls -laT -- "+q)
		if err != nil {
			return nil, vfs.WrapErr("list", dir, err)
		}
		return parseLs(out, "T"), nil
	case "custom":
		out, err := f.run(ctx, "cd -- "+q+" && LC_ALL=C "+f.opts.ListCommand)
		if err != nil {
			return nil, vfs.WrapErr("list", dir, err)
		}
		return parseLs(out, ""), nil
	default:
		out, err := f.run(ctx, "LC_ALL=C ls -la -- "+q)
		if err != nil {
			return nil, vfs.WrapErr("list", dir, err)
		}
		return parseLs(out, ""), nil
	}
}

func parseFind(out string) []vfs.Entry {
	var res []vfs.Entry
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		rec = strings.TrimPrefix(rec, "\n")
		p := strings.SplitN(rec, "\t", 11)
		if len(p) < 11 {
			continue
		}
		mode, _ := strconv.ParseUint(p[2], 8, 32)
		size, _ := strconv.ParseInt(p[3], 10, 64)
		ts, _ := strconv.ParseFloat(p[4], 64)
		uid, _ := strconv.Atoi(p[7])
		gid, _ := strconv.Atoi(p[8])
		sec := int64(ts)
		e := vfs.Entry{
			Name:    p[10],
			Mode:    uint32(mode),
			Size:    size,
			ModTime: time.Unix(sec, int64((ts-float64(sec))*1e9)),
			Owner:   p[5],
			Group:   p[6],
			UID:     uid,
			GID:     gid,
			IsLink:  p[0] == "l",
			Hidden:  strings.HasPrefix(p[10], "."),
		}
		e.IsDir = p[0] == "d" || (p[0] == "l" && p[1] == "d")
		if e.IsLink {
			e.LinkTarget = p[9]
		}
		if e.IsDir && !e.IsLink {
			e.Size = 0
		}
		res = append(res, e)
	}
	return res
}

var months = map[string]time.Month{"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6, "Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12}

// parseLs parses "ls -la" output. style: "full" (--full-time), "T" (BSD -T) or "" (classic).
func parseLs(out, style string) []vfs.Entry {
	var res []vfs.Entry
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		ln := sc.Text()
		if e, ok := ParseLsLine(ln, style); ok {
			if e.Name == "." || e.Name == ".." {
				continue
			}
			res = append(res, e)
		}
	}
	return res
}

// ParseLsLine parses one long-listing line (also used by the FTP backend).
func ParseLsLine(ln, style string) (vfs.Entry, bool) {
	if len(ln) < 10 || strings.HasPrefix(ln, "total ") {
		return vfs.Entry{}, false
	}
	fields := strings.Fields(ln)
	if len(fields) < 8 {
		return vfs.Entry{}, false
	}
	perm := fields[0]
	if len(perm) < 10 || !strings.ContainsRune("-dlcbps", rune(perm[0])) {
		return vfs.Entry{}, false
	}
	e := vfs.Entry{Mode: parsePerm(perm), IsDir: perm[0] == 'd', IsLink: perm[0] == 'l'}
	// fields: perm links owner group size <date...> name
	idx := 1
	if _, err := strconv.Atoi(fields[idx]); err == nil {
		idx++
	}
	if len(fields) < idx+4 {
		return vfs.Entry{}, false
	}
	e.Owner = fields[idx]
	idx++
	// some servers omit the group: detect by checking whether the next field is numeric followed by a date
	if _, err := strconv.ParseInt(fields[idx], 10, 64); err == nil {
		if _, err2 := strconv.ParseInt(fields[idx+1], 10, 64); err2 != nil {
			// group omitted
		} else {
			e.Group = fields[idx]
			idx++
		}
	} else {
		e.Group = fields[idx]
		idx++
	}
	if idx >= len(fields) {
		return vfs.Entry{}, false
	}
	sizeStr := strings.TrimSuffix(fields[idx], ",")
	if perm[0] == 'c' || perm[0] == 'b' { // device: "major, minor"
		idx++
		sizeStr = "0"
	}
	e.Size, _ = strconv.ParseInt(sizeStr, 10, 64)
	idx++
	var nameStart int
	switch {
	case style == "full" && idx+3 <= len(fields):
		t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700", fields[idx]+" "+fields[idx+1]+" "+fields[idx+2])
		if err != nil {
			t, _ = time.Parse("2006-01-02 15:04:05 -0700", fields[idx]+" "+fields[idx+1]+" "+fields[idx+2])
		}
		e.ModTime = t
		nameStart = idx + 3
	case style == "T" && idx+4 <= len(fields):
		t, _ := time.ParseInLocation("Jan 2 15:04:05 2006", strings.Join(fields[idx:idx+4], " "), time.Local)
		e.ModTime = t
		nameStart = idx + 4
	default:
		if idx+3 > len(fields) {
			return vfs.Entry{}, false
		}
		e.ModTime = parseClassicDate(fields[idx], fields[idx+1], fields[idx+2])
		nameStart = idx + 3
		// ISO style "2024-01-02 03:04"
		if strings.Count(fields[idx], "-") == 2 && strings.Contains(fields[idx+1], ":") {
			t, err := time.ParseInLocation("2006-01-02 15:04", fields[idx]+" "+fields[idx+1], time.Local)
			if err == nil {
				e.ModTime = t
				nameStart = idx + 2
			}
		}
	}
	if nameStart > len(fields) {
		return vfs.Entry{}, false
	}
	// Re-locate the name in the original line to preserve spaces.
	name := restAfterFields(ln, nameStart)
	if e.IsLink {
		if i := strings.Index(name, " -> "); i >= 0 {
			e.LinkTarget = name[i+4:]
			name = name[:i]
		}
	}
	e.Name = name
	e.Hidden = strings.HasPrefix(name, ".")
	if e.IsDir {
		e.Size = 0
	}
	return e, name != ""
}

func restAfterFields(ln string, n int) string {
	i := 0
	for f := 0; f < n; f++ {
		for i < len(ln) && (ln[i] == ' ' || ln[i] == '\t') {
			i++
		}
		for i < len(ln) && ln[i] != ' ' && ln[i] != '\t' {
			i++
		}
	}
	if i < len(ln) && (ln[i] == ' ' || ln[i] == '\t') {
		i++
	}
	return ln[i:]
}

func parseClassicDate(mon, day, yt string) time.Time {
	m, ok := months[strings.Title(strings.ToLower(mon))]
	if !ok {
		return time.Time{}
	}
	d, _ := strconv.Atoi(day)
	now := time.Now()
	if strings.Contains(yt, ":") {
		hm := strings.SplitN(yt, ":", 2)
		h, _ := strconv.Atoi(hm[0])
		mi, _ := strconv.Atoi(hm[1])
		t := time.Date(now.Year(), m, d, h, mi, 0, 0, time.Local)
		if t.After(now.Add(24 * time.Hour)) {
			t = t.AddDate(-1, 0, 0)
		}
		return t
	}
	y, _ := strconv.Atoi(yt)
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func parsePerm(p string) uint32 {
	var m uint32
	bits := []uint32{0o400, 0o200, 0o100, 0o040, 0o020, 0o010, 0o004, 0o002, 0o001}
	for i := 0; i < 9 && i+1 < len(p); i++ {
		c := p[i+1]
		if c != '-' && c != 'S' && c != 'T' {
			m |= bits[i]
		}
		switch {
		case i == 2 && (c == 's' || c == 'S'):
			m |= 0o4000
		case i == 5 && (c == 's' || c == 'S'):
			m |= 0o2000
		case i == 8 && (c == 't' || c == 'T'):
			m |= 0o1000
		}
	}
	return m
}

func (f *FS) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	q := vfs.ShellQuote(p)
	if f.detect(ctx) == "find" {
		out, err := f.run(ctx, "find "+q+" -maxdepth 0 -printf '"+findFmt+"'")
		if err != nil {
			return vfs.Entry{}, vfs.WrapErr("stat", p, err)
		}
		es := parseFind(out)
		if len(es) == 0 {
			return vfs.Entry{}, vfs.WrapErr("stat", p, vfs.ErrNotExist)
		}
		es[0].Name = path.Base(p)
		return es[0], nil
	}
	flag, style := "-lad", ""
	switch f.listMode {
	case "lsfull":
		flag, style = "-lad --full-time", "full"
	case "lsT":
		flag, style = "-ladT", "T"
	}
	out, err := f.run(ctx, "LC_ALL=C ls "+flag+" -- "+q)
	if err != nil {
		return vfs.Entry{}, vfs.WrapErr("stat", p, err)
	}
	e, ok := ParseLsLine(strings.TrimSpace(out), style)
	if !ok {
		return vfs.Entry{}, vfs.WrapErr("stat", p, errors.New("cannot parse listing"))
	}
	e.Name = path.Base(p)
	return e, nil
}

// scpReader streams one file sent by "scp -f".
type scpReader struct {
	sess      *ssh.Session
	r         *bufio.Reader
	w         io.WriteCloser
	remaining int64
	done      bool
}

func (s *scpReader) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		if !s.done {
			s.done = true
			if err := readAck(s.r); err != nil {
				return 0, err
			}
			s.w.Write([]byte{0})
		}
		return 0, io.EOF
	}
	if int64(len(p)) > s.remaining {
		p = p[:s.remaining]
	}
	n, err := s.r.Read(p)
	s.remaining -= int64(n)
	if err == io.EOF && s.remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

func (s *scpReader) Close() error {
	s.w.Close()
	if s.remaining > 0 {
		s.sess.Signal(ssh.SIGKILL)
	}
	s.sess.Close()
	return nil
}

func readAck(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	if b == 0 {
		return nil
	}
	msg, _ := r.ReadString('\n')
	return fmt.Errorf("scp: %s", strings.TrimSpace(msg))
}

func (f *FS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	if f.execFn != nil {
		return f.execOpen(ctx, p, offset)
	}
	if offset > 0 {
		// SCP cannot resume: stream the tail of the file through the shell.
		sess, err := f.conn.NewSession()
		if err != nil {
			return nil, err
		}
		out, err := sess.StdoutPipe()
		if err != nil {
			sess.Close()
			return nil, err
		}
		if err := sess.Start(f.wrap(fmt.Sprintf("tail -c +%d -- %s", offset+1, vfs.ShellQuote(p)))); err != nil {
			sess.Close()
			return nil, err
		}
		return &sessReader{sess: sess, r: out}, nil
	}
	sess, err := f.conn.NewSession()
	if err != nil {
		return nil, err
	}
	w, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	out, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.Start(f.wrap("scp -f -- " + vfs.ShellQuote(p))); err != nil {
		sess.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(out, 64*1024)
	fail := func(err error) (io.ReadCloser, error) {
		w.Close()
		sess.Close()
		return nil, vfs.WrapErr("download", p, err)
	}
	if _, err := w.Write([]byte{0}); err != nil {
		return fail(err)
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if line == "" {
				return fail(fmt.Errorf("scp: no data (%v)", err))
			}
		}
		if len(line) == 0 {
			return fail(errors.New("scp: empty response"))
		}
		switch line[0] {
		case 'T':
			w.Write([]byte{0})
			continue
		case 'C':
			parts := strings.SplitN(strings.TrimSpace(line[1:]), " ", 3)
			if len(parts) < 3 {
				return fail(fmt.Errorf("scp: bad header %q", line))
			}
			size, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return fail(err)
			}
			w.Write([]byte{0})
			return &scpReader{sess: sess, r: r, w: w, remaining: size}, nil
		case 'D':
			return fail(vfs.ErrIsDir)
		case 1, 2:
			msg := strings.TrimSpace(line[1:])
			if strings.Contains(strings.ToLower(msg), "no such file") {
				return fail(fmt.Errorf("%s: %w", msg, vfs.ErrNotExist))
			}
			if strings.Contains(strings.ToLower(msg), "not a regular file") {
				return fail(vfs.ErrIsDir)
			}
			return fail(errors.New(msg))
		default:
			// banner or garbage printed by the login shell: skip
			continue
		}
	}
}

type sessReader struct {
	sess *ssh.Session
	r    io.Reader
}

func (s *sessReader) Read(p []byte) (int, error) { return s.r.Read(p) }
func (s *sessReader) Close() error {
	s.sess.Signal(ssh.SIGKILL)
	return s.sess.Close()
}

// scpWriter uploads through "scp -t".
type scpWriter struct {
	f       *FS
	p       string
	mode    uint32
	size    int64
	written int64
	sess    *ssh.Session
	w       io.WriteCloser
	r       *bufio.Reader
	tmp     *os.File // used when the size is unknown
	ctx     context.Context
	started bool
}

func (s *scpWriter) start() error {
	sess, err := s.f.conn.NewSession()
	if err != nil {
		return err
	}
	w, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return err
	}
	out, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return err
	}
	if err := sess.Start(s.f.wrap("scp -t -- " + vfs.ShellQuote(s.p))); err != nil {
		sess.Close()
		return err
	}
	s.sess, s.w, s.r = sess, w, bufio.NewReader(out)
	if err := readAck(s.r); err != nil {
		sess.Close()
		return err
	}
	mode := s.mode & 0o7777
	if mode == 0 {
		mode = 0o644
	}
	if _, err := fmt.Fprintf(w, "C%04o %d %s\n", mode, s.size, path.Base(s.p)); err != nil {
		sess.Close()
		return err
	}
	if err := readAck(s.r); err != nil {
		sess.Close()
		return err
	}
	s.started = true
	return nil
}

func (s *scpWriter) Write(p []byte) (int, error) {
	if s.tmp != nil {
		return s.tmp.Write(p)
	}
	if !s.started {
		if err := s.start(); err != nil {
			return 0, err
		}
	}
	if s.written+int64(len(p)) > s.size {
		return 0, errors.New("scp: more data than announced")
	}
	n, err := s.w.Write(p)
	s.written += int64(n)
	return n, err
}

func (s *scpWriter) Close() error {
	if s.tmp != nil {
		defer os.Remove(s.tmp.Name())
		defer s.tmp.Close()
		st, err := s.tmp.Stat()
		if err != nil {
			return err
		}
		s.size = st.Size()
		if _, err := s.tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		src := s.tmp
		s.tmp = nil
		if err := s.start(); err != nil {
			return err
		}
		if _, err := io.Copy(writerOnly{s}, src); err != nil {
			s.sess.Close()
			return err
		}
	}
	if !s.started {
		if err := s.start(); err != nil {
			return err
		}
	}
	defer s.sess.Close()
	if s.written != s.size {
		s.sess.Signal(ssh.SIGKILL)
		return fmt.Errorf("scp: wrote %d of %d bytes", s.written, s.size)
	}
	if _, err := s.w.Write([]byte{0}); err != nil {
		return err
	}
	if err := readAck(s.r); err != nil {
		return err
	}
	s.w.Close()
	s.sess.Wait()
	return nil
}

type writerOnly struct{ w io.Writer }

func (w writerOnly) Write(p []byte) (int, error) { return w.w.Write(p) }

func (f *FS) Create(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	if f.execFn != nil {
		return f.execCreate(ctx, p, opt)
	}
	if opt.Offset > 0 {
		return nil, vfs.WrapErr("resume", p, vfs.ErrUnsupported)
	}
	w := &scpWriter{f: f, p: p, mode: opt.Mode, size: opt.Size, ctx: ctx}
	if opt.Size < 0 {
		tmp, err := os.CreateTemp("", "skiff-scp-*")
		if err != nil {
			return nil, err
		}
		w.tmp = tmp
	}
	return w, nil
}

func (f *FS) Mkdir(ctx context.Context, p string) error {
	_, err := f.run(ctx, "mkdir -- "+vfs.ShellQuote(p))
	return vfs.WrapErr("mkdir", p, err)
}

func (f *FS) Remove(ctx context.Context, p string, isDir bool) error {
	cmd := "rm -f -- "
	if isDir {
		cmd = "rmdir -- "
	}
	_, err := f.run(ctx, cmd+vfs.ShellQuote(p))
	return vfs.WrapErr("remove", p, err)
}

func (f *FS) RemoveAll(ctx context.Context, p string) error {
	if p == "/" || p == "" {
		return errors.New("refusing to delete the root directory")
	}
	_, err := f.run(ctx, "rm -rf -- "+vfs.ShellQuote(p))
	return vfs.WrapErr("remove", p, err)
}

func (f *FS) Rename(ctx context.Context, from, to string) error {
	_, err := f.run(ctx, "mv -f -- "+vfs.ShellQuote(from)+" "+vfs.ShellQuote(to))
	return vfs.WrapErr("rename", from, err)
}

func (f *FS) Chmod(ctx context.Context, p string, mode uint32) error {
	_, err := f.run(ctx, fmt.Sprintf("chmod %04o -- %s", mode&0o7777, vfs.ShellQuote(p)))
	return vfs.WrapErr("chmod", p, err)
}

func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	q := vfs.ShellQuote(p)
	u := mtime.UTC()
	cmd := fmt.Sprintf("touch -m -d @%d -- %s 2>/dev/null || TZ=UTC touch -m -t %s -- %s", mtime.Unix(), q, u.Format("200601021504.05"), q)
	_, err := f.run(ctx, cmd)
	return vfs.WrapErr("touch", p, err)
}

func (f *FS) Chown(ctx context.Context, p, owner, group string) error {
	q := vfs.ShellQuote(p)
	var cmd string
	switch {
	case owner != "" && group != "":
		cmd = "chown " + vfs.ShellQuote(owner+":"+group) + " -- " + q
	case owner != "":
		cmd = "chown " + vfs.ShellQuote(owner) + " -- " + q
	case group != "":
		cmd = "chgrp " + vfs.ShellQuote(group) + " -- " + q
	default:
		return nil
	}
	_, err := f.run(ctx, cmd)
	return vfs.WrapErr("chown", p, err)
}

func (f *FS) Symlink(ctx context.Context, target, link string, hard bool) error {
	flag := "-s "
	if hard {
		flag = ""
	}
	_, err := f.run(ctx, "ln "+flag+"-- "+vfs.ShellQuote(target)+" "+vfs.ShellQuote(link))
	return vfs.WrapErr("ln", link, err)
}

func (f *FS) Copy(ctx context.Context, from, to string) error {
	_, err := f.run(ctx, "cp -pR -- "+vfs.ShellQuote(from)+" "+vfs.ShellQuote(to))
	return vfs.WrapErr("cp", from, err)
}

func (f *FS) Checksum(ctx context.Context, p, algo string) (string, error) {
	return sftpfs.ShellChecksum(ctx, f, p, algo)
}

func (f *FS) Space(ctx context.Context, p string) (int64, int64, error) {
	out, err := f.run(ctx, "LC_ALL=C df -Pk -- "+vfs.ShellQuote(p))
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, 0, errors.New("cannot parse df output")
	}
	fs := strings.Fields(lines[len(lines)-1])
	if len(fs) < 4 {
		return 0, 0, errors.New("cannot parse df output")
	}
	total, _ := strconv.ParseInt(fs[1], 10, 64)
	avail, _ := strconv.ParseInt(fs[3], 10, 64)
	return total * 1024, avail * 1024, nil
}

func (f *FS) Fork(ctx context.Context) (vfs.FS, error) {
	f.mu.Lock()
	mode := f.listMode
	f.mu.Unlock()
	nf := &FS{conn: f.conn, opts: f.opts, fork: true, listMode: mode, home: f.home, execFn: f.execFn, exec: f.exec}
	return nf, nil
}

func (f *FS) Info() []vfs.InfoItem {
	if f.exec != nil {
		return append(append([]vfs.InfoItem(nil), f.exec.Info...), vfs.InfoItem{Group: "Container", Key: "Listing method", Value: f.listMode})
	}
	out := sftpfs.SSHInfo(f.conn.Info)
	out = append(out, vfs.InfoItem{Group: "SCP", Key: "Listing method", Value: f.listMode})
	if f.opts.Shell != "" {
		out = append(out, vfs.InfoItem{Group: "SCP", Key: "Shell", Value: f.opts.Shell})
	}
	return out
}

func (f *FS) Close() error {
	if f.fork {
		return nil
	}
	if f.exec != nil {
		if f.exec.Close != nil {
			return f.exec.Close()
		}
		return nil
	}
	return f.conn.Close()
}
