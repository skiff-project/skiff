package transfer

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skiff/internal/vfs"
	"skiff/internal/vfs/localfs"
)

// hostile is a server whose listing of dir holds names made to write
// outside of the destination.
type hostile struct {
	*localfs.FS
	dir string
}

var hostileNames = []string{"../evil.txt", "a/../../evil2.txt", ".."}

func (h hostile) List(ctx context.Context, d string) ([]vfs.Entry, error) {
	es, err := h.FS.List(ctx, d)
	if d == h.dir {
		for _, n := range hostileNames {
			es = append(es, vfs.Entry{Name: n, Size: 5})
		}
	}
	return es, err
}

// Open serves its made-up files whatever the path asked (the client joins
// and cleans the names its own way).
func (h hostile) Open(ctx context.Context, p string, off int64) (io.ReadCloser, error) {
	if r, err := h.FS.Open(ctx, p, off); err == nil {
		return r, nil
	}
	return io.NopCloser(strings.NewReader("pwned")), nil
}

func (h hostile) Stat(ctx context.Context, p string) (vfs.Entry, error) {
	if e, err := h.FS.Stat(ctx, p); err == nil {
		return e, nil
	}
	return vfs.Entry{Name: vfs.Base("posix", p), Size: 5}, nil
}

type hostileRes struct{ src, dst vfs.FS }

func (r hostileRes) Acquire(ctx context.Context, conn string) (vfs.FS, func(), error) {
	if conn == "server" {
		return r.src, func() {}, nil
	}
	return r.dst, func() {}, nil
}
func (r hostileRes) Title(conn string) string { return conn }
func (r hostileRes) Changed(conn, dir string) {}

func TestHostileNamesStayInTheDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "server", "share")
	dst := filepath.Join(root, "here", "downloads")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	os.WriteFile(filepath.Join(src, "ok.txt"), []byte("fine"), 0o644)
	m := NewManager(hostileRes{hostile{&localfs.FS{}, src}, &localfs.FS{}}, nil, nil, 1)
	defer m.Close()
	snap, _ := m.Run(context.Background(), Request{Src: "server", Dst: "local", Paths: []string{src}, DstDir: dst, Options: Options{Overwrite: "overwrite"}})
	if b, _ := os.ReadFile(filepath.Join(dst, "share", "ok.txt")); string(b) != "fine" {
		t.Errorf("the plain file was not downloaded (%+v)", snap)
	}
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			if b, _ := os.ReadFile(p); string(b) == "pwned" {
				t.Errorf("a hostile name was written: %s", p)
			}
		}
		return nil
	})
	if snap.FailedFiles != len(hostileNames) {
		t.Errorf("failed files = %d, want %d (%+v)", snap.FailedFiles, len(hostileNames), snap.Errors)
	}
}
