// Package sftpfs implements vfs.FS over SFTP (SSH File Transfer Protocol).
package sftpfs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"skiff/internal/sshx"
	"skiff/internal/vfs"
)

type Options struct {
	// ServerCommand runs a custom SFTP server instead of the "sftp"
	// subsystem, e.g. "sudo /usr/lib/openssh/sftp-server".
	ServerCommand string
	// ResolveSymlinks stats link targets to show them as dirs/files.
	ResolveSymlinks bool
	MaxPacket       int
	Logf            func(string, ...any)
}

type FS struct {
	conn   *sshx.Conn
	client *sftp.Client
	opts   Options
	fork   bool

	idsOnce sync.Once
	users   map[int]string
	groups  map[int]string
	execOK  bool
	homeDir string
}

func newClient(conn *sshx.Conn, opts Options) (*sftp.Client, error) {
	copts := []sftp.ClientOption{
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	}
	if opts.MaxPacket > 0 {
		copts = append(copts, sftp.MaxPacketUnchecked(opts.MaxPacket))
	}
	if strings.TrimSpace(opts.ServerCommand) == "" {
		return sftp.NewClient(conn.Client, copts...)
	}
	sess, err := conn.NewSession()
	if err != nil {
		return nil, err
	}
	w, err := sess.StdinPipe()
	if err != nil {
		return nil, err
	}
	r, err := sess.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := sess.Start(opts.ServerCommand); err != nil {
		return nil, fmt.Errorf("start %q: %w", opts.ServerCommand, err)
	}
	c, err := sftp.NewClientPipe(r, w, copts...)
	if err != nil {
		sess.Close()
		return nil, err
	}
	go func() {
		c.Wait()
		sess.Close()
	}()
	return c, nil
}

// New opens an SFTP session on an established SSH connection. The FS takes
// ownership of conn and closes it on Close.
func New(conn *sshx.Conn, opts Options) (*FS, error) {
	c, err := newClient(conn, opts)
	if err != nil {
		return nil, fmt.Errorf("sftp: %w", err)
	}
	return &FS{conn: conn, client: c, opts: opts}, nil
}

func (f *FS) SSH() *sshx.Conn { return f.conn }

func (f *FS) Caps() vfs.Caps {
	_, hl := f.client.HasExtension("hardlink@openssh.com")
	_, sv := f.client.HasExtension("statvfs@openssh.com")
	return vfs.Caps{
		Protocol:       "sftp",
		PathStyle:      "posix",
		Permissions:    true,
		Chmod:          true,
		Chown:          true,
		Owners:         true,
		Symlink:        true,
		Hardlink:       hl,
		SetMtime:       true,
		Exec:           true,
		Shell:          true,
		ServerCopy:     true,
		Checksum:       true,
		Space:          sv,
		ResumeDownload: true,
		ResumeUpload:   true,
		Tunnels:        true,
		MtimePrecision: time.Second,
	}
}

func (f *FS) Home(ctx context.Context) (string, error) {
	if f.homeDir != "" {
		return f.homeDir, nil
	}
	h, err := f.client.Getwd()
	if err != nil || h == "" {
		h, err = f.client.RealPath(".")
	}
	if err != nil {
		return "/", nil
	}
	f.homeDir = h
	return h, nil
}

func (f *FS) loadIDs() {
	f.idsOnce.Do(func() {
		f.users, f.groups = map[int]string{}, map[int]string{}
		var out bytes.Buffer
		code, err := f.Exec(context.Background(), "getent passwd 2>/dev/null || cat /etc/passwd; echo '--skiff--'; getent group 2>/dev/null || cat /etc/group", nil, &out, io.Discard)
		if err != nil || code != 0 {
			return
		}
		f.execOK = true
		target := f.users
		sc := bufio.NewScanner(&out)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			ln := sc.Text()
			if ln == "--skiff--" {
				target = f.groups
				continue
			}
			parts := strings.Split(ln, ":")
			if len(parts) < 3 {
				continue
			}
			if id, err := strconv.Atoi(parts[2]); err == nil {
				if _, dup := target[id]; !dup {
					target[id] = parts[0]
				}
			}
		}
	})
}

func (f *FS) toEntry(name string, fi os.FileInfo) vfs.Entry {
	e := vfs.Entry{
		Name:    name,
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
		IsLink:  fi.Mode()&os.ModeSymlink != 0,
		Hidden:  strings.HasPrefix(name, "."),
	}
	if st, ok := fi.Sys().(*sftp.FileStat); ok {
		e.Mode = st.Mode & 0o7777
		e.UID, e.GID = int(st.UID), int(st.GID)
		f.loadIDs()
		if n, ok := f.users[e.UID]; ok {
			e.Owner = n
		} else {
			e.Owner = strconv.Itoa(e.UID)
		}
		if n, ok := f.groups[e.GID]; ok {
			e.Group = n
		} else {
			e.Group = strconv.Itoa(e.GID)
		}
	} else {
		e.Mode = uint32(fi.Mode().Perm())
	}
	if e.IsDir {
		e.Size = 0
	}
	return e
}

func (f *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	fis, err := f.client.ReadDirContext(ctx, dir)
	if err != nil {
		return nil, vfs.WrapErr("list", dir, err)
	}
	out := make([]vfs.Entry, 0, len(fis))
	var links []int
	for _, fi := range fis {
		n := fi.Name()
		if n == "." || n == ".." {
			continue
		}
		e := f.toEntry(n, fi)
		if e.IsLink {
			links = append(links, len(out))
		}
		out = append(out, e)
	}
	if len(links) > 0 && f.opts.ResolveSymlinks {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 16)
		for _, i := range links {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				p := path.Join(dir, out[i].Name)
				if t, err := f.client.ReadLink(p); err == nil {
					out[i].LinkTarget = t
				}
				if st, err := f.client.Stat(p); err == nil {
					out[i].IsDir = st.IsDir()
					if !st.IsDir() {
						out[i].Size = st.Size()
					}
				}
			}(i)
		}
		wg.Wait()
	}
	return out, nil
}

func (f *FS) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	fi, err := f.client.Lstat(p)
	if err != nil {
		return vfs.Entry{}, vfs.WrapErr("stat", p, err)
	}
	e := f.toEntry(path.Base(p), fi)
	if e.IsLink {
		if t, err := f.client.ReadLink(p); err == nil {
			e.LinkTarget = t
		}
		if st, err := f.client.Stat(p); err == nil {
			e.IsDir = st.IsDir()
			if !st.IsDir() {
				e.Size = st.Size()
			}
		}
	}
	return e, nil
}

func (f *FS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	fh, err := f.client.Open(p)
	if err != nil {
		return nil, vfs.WrapErr("open", p, err)
	}
	if offset > 0 {
		if _, err := fh.Seek(offset, io.SeekStart); err != nil {
			fh.Close()
			return nil, err
		}
	}
	return fh, nil
}

// Writer wraps *sftp.File so that ReadFrom uses concurrent writes even when
// the source size cannot be detected by pkg/sftp.
type writer struct {
	*sftp.File
}

func (w writer) ReadFrom(r io.Reader) (int64, error) {
	return w.File.ReadFromWithConcurrency(r, 0)
}

func (f *FS) Create(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	var fh *sftp.File
	var err error
	if opt.Offset > 0 {
		fh, err = f.client.OpenFile(p, os.O_WRONLY)
		if err == nil {
			if err = fh.Truncate(opt.Offset); err == nil {
				_, err = fh.Seek(opt.Offset, io.SeekStart)
			}
			if err != nil {
				fh.Close()
			}
		}
	} else {
		fh, err = f.client.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	}
	if err != nil {
		return nil, vfs.WrapErr("create", p, err)
	}
	if opt.Mode != 0 && opt.Offset == 0 {
		fh.Chmod(os.FileMode(opt.Mode & 0o777))
	}
	return writer{fh}, nil
}

// WriteAt opens an existing file for writing at offset (segmented transfers).
func (f *FS) WriteAt(ctx context.Context, p string, offset int64) (io.WriteCloser, error) {
	fh, err := f.client.OpenFile(p, os.O_WRONLY)
	if err != nil {
		return nil, vfs.WrapErr("open", p, err)
	}
	if _, err := fh.Seek(offset, io.SeekStart); err != nil {
		fh.Close()
		return nil, err
	}
	return writer{fh}, nil
}

func (f *FS) Mkdir(ctx context.Context, p string) error {
	return vfs.WrapErr("mkdir", p, f.client.Mkdir(p))
}

func (f *FS) Remove(ctx context.Context, p string, isDir bool) error {
	if isDir {
		return vfs.WrapErr("rmdir", p, f.client.RemoveDirectory(p))
	}
	return vfs.WrapErr("remove", p, f.client.Remove(p))
}

func (f *FS) RemoveAll(ctx context.Context, p string) error {
	if p == "/" || p == "" {
		return errors.New("refusing to delete the root directory")
	}
	// Fast path through the shell when available.
	if f.canExec() {
		var errb bytes.Buffer
		code, err := f.Exec(ctx, "rm -rf -- "+vfs.ShellQuote(p), nil, io.Discard, &errb)
		if err == nil && code == 0 {
			return nil
		}
	}
	return removeAllSFTP(ctx, f, p)
}

func removeAllSFTP(ctx context.Context, f *FS, p string) error {
	fis, err := f.client.ReadDirContext(ctx, p)
	if err != nil {
		return vfs.WrapErr("list", p, err)
	}
	for _, fi := range fis {
		if fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		c := path.Join(p, fi.Name())
		if fi.IsDir() {
			if err := removeAllSFTP(ctx, f, c); err != nil {
				return err
			}
		} else if err := f.client.Remove(c); err != nil {
			return vfs.WrapErr("remove", c, err)
		}
	}
	return vfs.WrapErr("rmdir", p, f.client.RemoveDirectory(p))
}

func (f *FS) Rename(ctx context.Context, from, to string) error {
	if _, ok := f.client.HasExtension("posix-rename@openssh.com"); ok {
		return vfs.WrapErr("rename", from, f.client.PosixRename(from, to))
	}
	err := f.client.Rename(from, to)
	if err != nil {
		// SFTPv3 rename fails when the target exists: emulate overwrite.
		if st, serr := f.client.Lstat(to); serr == nil && !st.IsDir() {
			if rerr := f.client.Remove(to); rerr == nil {
				err = f.client.Rename(from, to)
			}
		}
	}
	return vfs.WrapErr("rename", from, err)
}

func (f *FS) Chmod(ctx context.Context, p string, mode uint32) error {
	// pkg/sftp maps os.FileMode bits; pass setuid/gid/sticky explicitly.
	m := os.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		m |= os.ModeSetuid
	}
	if mode&0o2000 != 0 {
		m |= os.ModeSetgid
	}
	if mode&0o1000 != 0 {
		m |= os.ModeSticky
	}
	return vfs.WrapErr("chmod", p, f.client.Chmod(p, m))
}

func (f *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	return vfs.WrapErr("chtimes", p, f.client.Chtimes(p, mtime, mtime))
}

func (f *FS) Chown(ctx context.Context, p, owner, group string) error {
	st, err := f.client.Lstat(p)
	if err != nil {
		return vfs.WrapErr("chown", p, err)
	}
	fs, _ := st.Sys().(*sftp.FileStat)
	uid, gid := -1, -1
	if fs != nil {
		uid, gid = int(fs.UID), int(fs.GID)
	}
	f.loadIDs()
	if owner != "" {
		if n, err := strconv.Atoi(owner); err == nil {
			uid = n
		} else if id, ok := lookup(f.users, owner); ok {
			uid = id
		} else {
			return fmt.Errorf("unknown user %q", owner)
		}
	}
	if group != "" {
		if n, err := strconv.Atoi(group); err == nil {
			gid = n
		} else if id, ok := lookup(f.groups, group); ok {
			gid = id
		} else {
			return fmt.Errorf("unknown group %q", group)
		}
	}
	return vfs.WrapErr("chown", p, f.client.Chown(p, uid, gid))
}

func lookup(m map[int]string, name string) (int, bool) {
	for id, n := range m {
		if n == name {
			return id, true
		}
	}
	return 0, false
}

// Owners lists known users/groups (for the properties dialog).
func (f *FS) Owners() (users, groups []string) {
	f.loadIDs()
	for _, n := range f.users {
		users = append(users, n)
	}
	for _, n := range f.groups {
		groups = append(groups, n)
	}
	sort.Strings(users)
	sort.Strings(groups)
	return
}

func (f *FS) Symlink(ctx context.Context, target, link string, hard bool) error {
	if hard {
		return vfs.WrapErr("link", link, f.client.Link(target, link))
	}
	return vfs.WrapErr("symlink", link, f.client.Symlink(target, link))
}

func (f *FS) canExec() bool {
	f.loadIDs()
	return f.execOK
}

func (f *FS) Exec(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return ExecSSH(ctx, f.conn.Client, cmd, stdin, stdout, stderr)
}

// ExecSSH runs cmd in a new SSH session and returns its exit status.
func ExecSSH(ctx context.Context, c *ssh.Client, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	sess, err := c.NewSession()
	if err != nil {
		return -1, err
	}
	defer sess.Close()
	sess.Stdin, sess.Stdout, sess.Stderr = stdin, stdout, stderr
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case err = <-done:
	case <-ctx.Done():
		sess.Signal(ssh.SIGKILL)
		sess.Close()
		return -1, ctx.Err()
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return ee.ExitStatus(), nil
	}
	var em *ssh.ExitMissingError
	if errors.As(err, &em) {
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (f *FS) Copy(ctx context.Context, from, to string) error {
	var errb bytes.Buffer
	code, err := f.Exec(ctx, "cp -pR -- "+vfs.ShellQuote(from)+" "+vfs.ShellQuote(to), nil, io.Discard, &errb)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("cp: %s", strings.TrimSpace(errb.String()))
	}
	return nil
}

func (f *FS) Checksum(ctx context.Context, p, algo string) (string, error) {
	return ShellChecksum(ctx, f, p, algo)
}

// ShellChecksum computes a checksum using the remote md5sum/sha*sum tools.
func ShellChecksum(ctx context.Context, ex vfs.Executor, p, algo string) (string, error) {
	tool := map[string]string{"md5": "md5sum", "sha1": "sha1sum", "sha256": "sha256sum", "sha512": "sha512sum", "": "sha256sum"}[strings.ToLower(strings.ReplaceAll(algo, "-", ""))]
	if tool == "" {
		return "", fmt.Errorf("unsupported algorithm %q", algo)
	}
	var out, errb bytes.Buffer
	q := vfs.ShellQuote(p)
	bsd := map[string]string{"md5sum": "md5 -q", "sha1sum": "shasum -a 1", "sha256sum": "shasum -a 256", "sha512sum": "shasum -a 512"}[tool]
	code, err := ex.Exec(ctx, tool+" -- "+q+" 2>/dev/null || "+bsd+" "+q, nil, &out, &errb)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("%s failed: %s", tool, strings.TrimSpace(errb.String()))
	}
	fields := strings.Fields(out.String())
	if len(fields) == 0 {
		return "", errors.New("empty checksum output")
	}
	return strings.ToLower(fields[0]), nil
}

func (f *FS) Space(ctx context.Context, p string) (int64, int64, error) {
	st, err := f.client.StatVFS(p)
	if err != nil {
		return 0, 0, err
	}
	return int64(st.TotalSpace()), int64(st.FreeSpace()), nil
}

// Fork opens a new SFTP channel on the same SSH connection.
func (f *FS) Fork(ctx context.Context) (vfs.FS, error) {
	c, err := newClient(f.conn, f.opts)
	if err != nil {
		return nil, err
	}
	nf := &FS{conn: f.conn, client: c, opts: f.opts, fork: true, homeDir: f.homeDir}
	f.loadIDs()
	nf.idsOnce.Do(func() {})
	nf.users, nf.groups, nf.execOK = f.users, f.groups, f.execOK
	return nf, nil
}

func (f *FS) Info() []vfs.InfoItem {
	i := f.conn.Info
	out := sshInfo(i)
	exts := []string{}
	for _, e := range []string{"posix-rename@openssh.com", "statvfs@openssh.com", "fstatvfs@openssh.com", "hardlink@openssh.com", "fsync@openssh.com", "lsetstat@openssh.com", "limits@openssh.com", "expand-path@openssh.com", "copy-data", "check-file"} {
		if _, ok := f.client.HasExtension(e); ok {
			exts = append(exts, e)
		}
	}
	out = append(out, vfs.InfoItem{Group: "SFTP", Key: "Protocol version", Value: "3"})
	out = append(out, vfs.InfoItem{Group: "SFTP", Key: "Extensions", Value: strings.Join(exts, ", ")})
	if f.opts.ServerCommand != "" {
		out = append(out, vfs.InfoItem{Group: "SFTP", Key: "Server command", Value: f.opts.ServerCommand})
	}
	return out
}

func sshInfo(i sshx.Info) []vfs.InfoItem {
	key := i.AuthKey
	if key != "" && i.AuthCert {
		key += " (certificate)"
	}
	return []vfs.InfoItem{
		{Group: "SSH", Key: "Server", Value: i.ServerVersion},
		{Group: "SSH", Key: "Address", Value: i.RemoteAddr},
		{Group: "SSH", Key: "Host key", Value: i.HostKeyType},
		{Group: "SSH", Key: "Fingerprint (SHA-256)", Value: i.FingerprintSHA256},
		{Group: "SSH", Key: "Fingerprint (MD5)", Value: i.FingerprintMD5},
		{Group: "SSH", Key: "Key exchange", Value: i.Kex},
		{Group: "SSH", Key: "Cipher (client → server)", Value: i.CipherCS},
		{Group: "SSH", Key: "Cipher (server → client)", Value: i.CipherSC},
		{Group: "SSH", Key: "MAC", Value: i.MACCS},
		{Group: "SSH", Key: "Authentication", Value: i.AuthMethod},
		{Group: "SSH", Key: "Authentication key", Value: key},
		{Group: "SSH", Key: "Jump hosts", Value: strconv.Itoa(i.Jumps)},
	}
}

// SSHInfo is exported for the SCP backend.
func SSHInfo(i sshx.Info) []vfs.InfoItem { return sshInfo(i) }

func (f *FS) IsSharedFork() bool { return false }

func (f *FS) Close() error {
	err := f.client.Close()
	if !f.fork {
		f.conn.Close()
	}
	return err
}
