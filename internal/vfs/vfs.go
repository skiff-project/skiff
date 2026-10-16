// Package vfs defines the unified file-system abstraction every protocol
// backend (local, SFTP, SCP, FTP/FTPS, WebDAV, S3) implements.
package vfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// Entry describes one file system object.
type Entry struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Mode       uint32    `json:"mode"` // permission bits incl. setuid/setgid/sticky (07777)
	ModTime    time.Time `json:"mtime"`
	IsDir      bool      `json:"isDir"`
	IsLink     bool      `json:"isLink,omitempty"`
	LinkTarget string    `json:"linkTarget,omitempty"`
	Owner      string    `json:"owner,omitempty"`
	Group      string    `json:"group,omitempty"`
	UID        int       `json:"uid,omitempty"`
	GID        int       `json:"gid,omitempty"`
	Hidden     bool      `json:"hidden,omitempty"`
	MimeType   string    `json:"mime,omitempty"`
	ETag       string    `json:"etag,omitempty"`
}

// Caps advertises what a backend supports so the UI and the engines can adapt.
type Caps struct {
	Protocol       string `json:"protocol"`
	PathStyle      string `json:"pathStyle"` // "posix" or "windows"
	Permissions    bool   `json:"permissions"`
	Chmod          bool   `json:"chmod"`
	Chown          bool   `json:"chown"`
	Owners         bool   `json:"owners"`
	Symlink        bool   `json:"symlink"`
	Hardlink       bool   `json:"hardlink"`
	SetMtime       bool   `json:"setMtime"`
	Exec           bool   `json:"exec"`
	Shell          bool   `json:"shell"` // interactive terminal possible
	ServerCopy     bool   `json:"serverCopy"`
	Checksum       bool   `json:"checksum"`
	Space          bool   `json:"space"`
	ResumeDownload bool   `json:"resumeDownload"`
	ResumeUpload   bool   `json:"resumeUpload"`
	Tunnels        bool   `json:"tunnels"`
	// MtimePrecision is the coarsest precision of reported modification times.
	MtimePrecision time.Duration `json:"mtimePrecision"`
}

// CreateOpts controls how a file is opened for writing.
type CreateOpts struct {
	// Offset > 0 resumes/appends at that offset (the file must already be that long).
	Offset int64
	// Size is the expected final size, or -1 when unknown.
	Size int64
	// Mode is the permission for a newly created file (0 = backend default).
	Mode uint32
	// ModTime, when non-zero, is applied at upload time by backends that can
	// only set it then (WebDAV X-OC-Mtime, S3 metadata).
	ModTime time.Time
}

// FS is the minimal interface each backend implements. Implementations must
// be safe for use by one goroutine at a time unless they also implement
// Forker returning themselves (meaning they are concurrency-safe).
type FS interface {
	Caps() Caps
	Home(ctx context.Context) (string, error)
	List(ctx context.Context, dir string) ([]Entry, error)
	Stat(ctx context.Context, p string) (Entry, error)
	Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error)
	Create(ctx context.Context, p string, opt CreateOpts) (io.WriteCloser, error)
	Mkdir(ctx context.Context, p string) error
	Remove(ctx context.Context, p string, isDir bool) error
	Rename(ctx context.Context, from, to string) error
	Chmod(ctx context.Context, p string, mode uint32) error
	Chtimes(ctx context.Context, p string, mtime time.Time) error
	Close() error
}

// Optional capabilities -------------------------------------------------------

type Chowner interface {
	Chown(ctx context.Context, p, owner, group string) error
}

type Linker interface {
	Symlink(ctx context.Context, target, link string, hard bool) error
}

// Executor runs a shell command on the remote side.
type Executor interface {
	Exec(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

type ServerCopier interface {
	Copy(ctx context.Context, from, to string) error
}

type Checksummer interface {
	Checksum(ctx context.Context, p, algo string) (string, error)
}

type SpaceReporter interface {
	Space(ctx context.Context, p string) (total, free int64, err error)
}

// Forker returns an independent handle suitable for concurrent use (for
// background transfers). Backends that are already concurrency-safe may
// return themselves together with a no-op closer semantics (CloseFork).
type Forker interface {
	Fork(ctx context.Context) (FS, error)
}

// SharedFork marks an FS returned by Fork that must not be closed by the
// caller because it is shared with the parent.
type SharedFork interface {
	IsSharedFork() bool
}

// RandomWriter is implemented by backends that can write one file at any
// offset through several handles at once (segmented transfers). WriteAt
// opens an existing file for writing at offset, without truncating it.
type RandomWriter interface {
	WriteAt(ctx context.Context, p string, offset int64) (io.WriteCloser, error)
}

type RemoveAller interface {
	RemoveAll(ctx context.Context, p string) error
}

// InfoItem is a key/value shown in the "server/protocol information" dialog.
type InfoItem struct {
	Group string `json:"group"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Infoer interface {
	Info() []InfoItem
}

// Errors ----------------------------------------------------------------------

var (
	ErrNotExist    = fs.ErrNotExist
	ErrExist       = fs.ErrExist
	ErrPermission  = fs.ErrPermission
	ErrUnsupported = errors.New("operation not supported by this protocol")
	ErrIsDir       = errors.New("is a directory")
)

// IsNotExist reports whether err means "no such file".
func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// PathError decorates an error with the operation and path.
type PathError struct {
	Op   string
	Path string
	Err  error
}

func (e *PathError) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }
func (e *PathError) Unwrap() error { return e.Err }

func WrapErr(op, p string, err error) error {
	if err == nil {
		return nil
	}
	var pe *PathError
	if errors.As(err, &pe) {
		return err
	}
	return &PathError{Op: op, Path: p, Err: err}
}
