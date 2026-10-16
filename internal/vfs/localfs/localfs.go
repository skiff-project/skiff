// Package localfs implements vfs.FS on top of the local operating system.
package localfs

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"skiff/internal/vfs"
)

type FS struct {
	// Root, when set, jails every path inside this directory (web mode).
	Root string
}

func New() *FS { return &FS{} }

func (l *FS) style() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "posix"
}

func (l *FS) Caps() vfs.Caps {
	unix := runtime.GOOS != "windows"
	return vfs.Caps{
		Protocol:       "local",
		PathStyle:      l.style(),
		Permissions:    unix,
		Chmod:          unix,
		Chown:          unix,
		Owners:         unix,
		Symlink:        true,
		Hardlink:       true,
		SetMtime:       true,
		Exec:           true,
		ServerCopy:     true,
		Checksum:       true,
		Space:          true,
		ResumeDownload: true,
		ResumeUpload:   true,
		MtimePrecision: time.Nanosecond,
	}
}

// ExpandHome turns "~" and "~/…" into absolute paths in the user's home.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(h, p[1:])
}

// resolve validates p and applies the optional jail.
func (l *FS) resolve(p string) (string, error) {
	if p == "" {
		return "", vfs.ErrNotExist
	}
	if l.Root == "" {
		return filepath.Clean(ExpandHome(p)), nil
	}
	clean := filepath.Clean(p)
	root := filepath.Clean(l.Root)
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	inside := func(x string) bool {
		return x == root || strings.HasPrefix(x, root+string(filepath.Separator))
	}
	if !inside(clean) {
		return "", vfs.WrapErr("access", p, vfs.ErrPermission)
	}
	// Resolve symbolic links so that a link inside the root cannot point
	// outside of it (the path itself, or its parent for new files).
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		if !inside(real) {
			return "", vfs.WrapErr("access", p, vfs.ErrPermission)
		}
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(clean)); err == nil {
		if !inside(real) {
			return "", vfs.WrapErr("access", p, vfs.ErrPermission)
		}
	}
	return clean, nil
}

func (l *FS) Home(ctx context.Context) (string, error) {
	if l.Root != "" {
		return filepath.Clean(l.Root), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return l.rootPath(), nil
	}
	return h, nil
}

func (l *FS) rootPath() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return "/"
}

func (l *FS) entry(name string, fi os.FileInfo, full string) vfs.Entry {
	e := vfs.Entry{
		Name:    name,
		Size:    fi.Size(),
		Mode:    unixMode(fi.Mode()),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		e.IsLink = true
		if t, err := os.Readlink(full); err == nil {
			e.LinkTarget = t
		}
		if st, err := os.Stat(full); err == nil {
			e.IsDir = st.IsDir()
			if !st.IsDir() {
				e.Size = st.Size()
			}
		}
	}
	if e.IsDir && !e.IsLink {
		e.Size = 0
	}
	fillOwner(&e, fi)
	e.Hidden = isHidden(name, fi)
	return e
}

func (l *FS) List(ctx context.Context, dir string) ([]vfs.Entry, error) {
	if runtime.GOOS == "windows" && (dir == "" || dir == "/") && l.Root == "" {
		return listDrives(), nil
	}
	d, err := l.resolve(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(d)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]vfs.Entry, 0, 64)
	for {
		des, err := f.ReadDir(512)
		for _, de := range des {
			full := filepath.Join(d, de.Name())
			fi, ierr := de.Info()
			if ierr != nil {
				continue
			}
			out = append(out, l.entry(de.Name(), fi, full))
		}
		if err == io.EOF || len(des) == 0 {
			break
		}
		if err != nil {
			return out, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return out, nil
}

func (l *FS) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	if runtime.GOOS == "windows" && (p == "" || p == "/") {
		return vfs.Entry{Name: "", IsDir: true}, nil
	}
	rp, err := l.resolve(p)
	if err != nil {
		return vfs.Entry{}, err
	}
	fi, err := os.Lstat(rp)
	if err != nil {
		return vfs.Entry{}, err
	}
	return l.entry(filepath.Base(rp), fi, rp), nil
}

func (l *FS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	rp, err := l.resolve(p)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(rp)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		f.Close()
		return nil, vfs.WrapErr("open", p, vfs.ErrIsDir)
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

func (l *FS) Create(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	rp, err := l.resolve(p)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(0o644)
	if opt.Mode != 0 {
		mode = os.FileMode(opt.Mode & 0o777)
	}
	if opt.Offset > 0 {
		f, err := os.OpenFile(rp, os.O_WRONLY, mode)
		if err != nil {
			return nil, err
		}
		if err := f.Truncate(opt.Offset); err != nil {
			f.Close()
			return nil, err
		}
		if _, err := f.Seek(opt.Offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	}
	return os.OpenFile(rp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
}

// WriteAt opens an existing file for writing at offset (segmented transfers).
func (l *FS) WriteAt(ctx context.Context, p string, offset int64) (io.WriteCloser, error) {
	rp, err := l.resolve(p)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(rp, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (l *FS) Mkdir(ctx context.Context, p string) error {
	rp, err := l.resolve(p)
	if err != nil {
		return err
	}
	return os.Mkdir(rp, 0o755)
}

func (l *FS) Remove(ctx context.Context, p string, isDir bool) error {
	rp, err := l.resolve(p)
	if err != nil {
		return err
	}
	return os.Remove(rp)
}

func (l *FS) RemoveAll(ctx context.Context, p string) error {
	rp, err := l.resolve(p)
	if err != nil {
		return err
	}
	if l.Root != "" && rp == filepath.Clean(l.Root) {
		return vfs.WrapErr("remove", p, vfs.ErrPermission)
	}
	return os.RemoveAll(rp)
}

func (l *FS) Rename(ctx context.Context, from, to string) error {
	a, err := l.resolve(from)
	if err != nil {
		return err
	}
	b, err := l.resolve(to)
	if err != nil {
		return err
	}
	if err := os.Rename(a, b); err != nil {
		var le *os.LinkError
		if errors.As(err, &le) && isCrossDevice(err) {
			if err := copyTree(a, b); err != nil {
				return err
			}
			return os.RemoveAll(a)
		}
		return err
	}
	return nil
}

func (l *FS) Chmod(ctx context.Context, p string, mode uint32) error {
	rp, err := l.resolve(p)
	if err != nil {
		return err
	}
	return os.Chmod(rp, goMode(mode))
}

func (l *FS) Chtimes(ctx context.Context, p string, mtime time.Time) error {
	rp, err := l.resolve(p)
	if err != nil {
		return err
	}
	return os.Chtimes(rp, time.Now(), mtime)
}

func (l *FS) Chown(ctx context.Context, p, owner, group string) error {
	rp, err := l.resolve(p)
	if err != nil {
		return err
	}
	return chown(rp, owner, group)
}

func (l *FS) Symlink(ctx context.Context, target, link string, hard bool) error {
	lp, err := l.resolve(link)
	if err != nil {
		return err
	}
	if hard {
		tp, err := l.resolve(target)
		if err != nil {
			return err
		}
		return os.Link(tp, lp)
	}
	return os.Symlink(target, lp)
}

func (l *FS) Copy(ctx context.Context, from, to string) error {
	a, err := l.resolve(from)
	if err != nil {
		return err
	}
	b, err := l.resolve(to)
	if err != nil {
		return err
	}
	return copyTree(a, b)
}

func (l *FS) Exec(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if l.Root != "" {
		return -1, vfs.ErrUnsupported
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	} else {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		c = exec.CommandContext(ctx, sh, "-c", cmd)
	}
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (l *FS) Checksum(ctx context.Context, p, algo string) (string, error) {
	rp, err := l.resolve(p)
	if err != nil {
		return "", err
	}
	h, err := NewHash(algo)
	if err != nil {
		return "", err
	}
	f, err := os.Open(rp)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, &ctxReader{ctx: ctx, r: f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (l *FS) Space(ctx context.Context, p string) (int64, int64, error) {
	rp, err := l.resolve(p)
	if err != nil {
		return 0, 0, err
	}
	return diskSpace(rp)
}

func (l *FS) Fork(ctx context.Context) (vfs.FS, error) { return sharedFork{l}, nil }

func (l *FS) Close() error { return nil }

type sharedFork struct{ *FS }

func (sharedFork) IsSharedFork() bool { return true }

// NewHash returns a hash for the given algorithm name.
func NewHash(algo string) (hash.Hash, error) {
	switch strings.ToLower(strings.ReplaceAll(algo, "-", "")) {
	case "md5":
		return md5.New(), nil
	case "sha1":
		return sha1.New(), nil
	case "sha256", "":
		return sha256.New(), nil
	case "sha512":
		return sha512.New(), nil
	}
	return nil, fmt.Errorf("unsupported checksum algorithm %q", algo)
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func unixMode(m os.FileMode) uint32 {
	out := uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		out |= 0o4000
	}
	if m&os.ModeSetgid != 0 {
		out |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		out |= 0o1000
	}
	return out
}

func goMode(m uint32) os.FileMode {
	out := os.FileMode(m & 0o777)
	if m&0o4000 != 0 {
		out |= os.ModeSetuid
	}
	if m&0o2000 != 0 {
		out |= os.ModeSetgid
	}
	if m&0o1000 != 0 {
		out |= os.ModeSticky
	}
	return out
}

func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		t, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(t, dst)
	case fi.IsDir():
		if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
			return err
		}
		des, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, de := range des {
			if err := copyTree(filepath.Join(src, de.Name()), filepath.Join(dst, de.Name())); err != nil {
				return err
			}
		}
		return os.Chtimes(dst, time.Now(), fi.ModTime())
	default:
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chtimes(dst, time.Now(), fi.ModTime())
	}
}
