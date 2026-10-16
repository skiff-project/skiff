package scpfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"skiff/internal/vfs"
)

// ExecFunc runs a shell command line where the files are and returns its
// exit status.
type ExecFunc func(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

// ExecOptions configure a file system driven by commands only.
type ExecOptions struct {
	Protocol string // reported in Caps, e.g. "container"
	Info     []vfs.InfoItem
	Close    func() error // releases the transport
	Logf     func(string, ...any)
}

// NewExec returns a file system that runs every operation as a shell
// command through run, with no SSH session nor scp: files are read with cat
// (tail -c to resume) and written with cat >. It is what a container offers
// through "docker exec" or "kubectl exec".
func NewExec(run ExecFunc, o ExecOptions) *FS {
	return &FS{execFn: run, exec: &o, opts: Options{Logf: o.Logf}}
}

func (f *FS) execCaps() vfs.Caps {
	return vfs.Caps{
		Protocol: f.exec.Protocol, PathStyle: "posix", Permissions: true, Chmod: true, Chown: true, Owners: true,
		Symlink: true, Hardlink: true, SetMtime: true, Exec: true, Shell: true, ServerCopy: true, Checksum: true,
		Space: true, ResumeDownload: true, MtimePrecision: time.Second,
	}
}

// execReader streams the standard output of a running command.
type execReader struct {
	*io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *execReader) Close() error {
	r.cancel()
	r.PipeReader.Close()
	<-r.done
	return nil
}

func (f *FS) execOpen(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	cmd := "cat -- " + vfs.ShellQuote(p)
	if offset > 0 {
		cmd = fmt.Sprintf("tail -c +%d -- %s", offset+1, vfs.ShellQuote(p))
	}
	// fail early for a missing file or a directory, like the other backends
	if _, err := f.run(ctx, "test -f "+vfs.ShellQuote(p)+" && test -r "+vfs.ShellQuote(p)); err != nil {
		if e, serr := f.Stat(ctx, p); serr != nil {
			return nil, serr
		} else if e.IsDir {
			return nil, vfs.WrapErr("open", p, vfs.ErrIsDir)
		}
		return nil, vfs.WrapErr("open", p, vfs.ErrPermission)
	}
	cctx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	r := &execReader{PipeReader: pr, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		var errb bytes.Buffer
		code, err := f.execFn(cctx, f.wrap(cmd), nil, pw, &errb)
		if err == nil && code != 0 {
			err = fmt.Errorf("%s: %s", cmd, strings.TrimSpace(errb.String()))
		}
		pw.CloseWithError(err)
	}()
	return r, nil
}

// execWriter feeds "cat > file" and reports its result on Close.
type execWriter struct {
	pw   *io.PipeWriter
	done chan error
	once sync.Once
	err  error
}

func (w *execWriter) Write(p []byte) (int, error) { return w.pw.Write(p) }

func (w *execWriter) Close() error {
	w.once.Do(func() {
		w.pw.Close()
		w.err = <-w.done
	})
	return w.err
}

func (f *FS) execCreate(ctx context.Context, p string, opt vfs.CreateOpts) (io.WriteCloser, error) {
	if opt.Offset > 0 {
		return nil, vfs.WrapErr("resume", p, vfs.ErrUnsupported)
	}
	q := vfs.ShellQuote(p)
	cmd := "cat > " + q
	if opt.Mode != 0 {
		cmd += fmt.Sprintf(" && chmod %04o -- %s", opt.Mode&0o7777, q)
	}
	pr, pw := io.Pipe()
	w := &execWriter{pw: pw, done: make(chan error, 1)}
	go func() {
		var errb bytes.Buffer
		code, err := f.execFn(ctx, f.wrap(cmd), pr, io.Discard, &errb)
		if err == nil && code != 0 {
			msg := strings.TrimSpace(errb.String())
			low := strings.ToLower(msg)
			switch {
			case strings.Contains(low, "permission denied"):
				err = vfs.WrapErr("create", p, fmt.Errorf("%s: %w", msg, vfs.ErrPermission))
			case strings.Contains(low, "no such file") || strings.Contains(low, "nonexistent"):
				err = vfs.WrapErr("create", p, fmt.Errorf("%s: %w", msg, vfs.ErrNotExist))
			default:
				err = vfs.WrapErr("create", p, fmt.Errorf("%s (exit %d)", msg, code))
			}
		}
		pr.CloseWithError(err) // stop the writer if the command ended early
		w.done <- err
	}()
	return w, nil
}
