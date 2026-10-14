package localfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"skiff/internal/vfs"
)

func TestJail(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600)
	os.Symlink(outside, filepath.Join(root, "link"))
	os.Mkdir(filepath.Join(root, "ok"), 0o755)
	f := &FS{Root: root}
	ctx := context.Background()
	if _, err := f.List(ctx, filepath.Join(root, "ok")); err != nil {
		t.Fatalf("inside: %v", err)
	}
	for _, p := range []string{"/etc", filepath.Join(root, ".."), filepath.Join(root, "link"), filepath.Join(root, "link", "secret")} {
		if _, err := f.List(ctx, p); err == nil {
			t.Errorf("%s: escaped the jail", p)
		}
		if _, err := f.Open(ctx, p, 0); err == nil {
			t.Errorf("%s: opened outside the jail", p)
		}
	}
	if _, err := f.Create(ctx, filepath.Join(root, "link", "new"), vfs.CreateOpts{Size: -1}); err == nil {
		t.Error("created a file through a symlink outside the jail")
	}
}
