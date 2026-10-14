package transfer

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/vfs"
	"skiff/internal/vfs/localfs"
)

type testRes struct{ fs *localfs.FS }

func (r testRes) Acquire(ctx context.Context, conn string) (vfs.FS, func(), error) {
	return r.fs, func() {}, nil
}
func (r testRes) Title(conn string) string  { return conn }
func (r testRes) Changed(conn, dir string)  {}

func TestEOLReader(t *testing.T) {
	cases := []struct{ in, from, to, want string }{
		{"a\nb\n", "LF", "CRLF", "a\r\nb\r\n"},
		{"a\r\nb\r\n", "CRLF", "LF", "a\nb\n"},
		{"a\rb", "CRLF", "LF", "a\rb"},
		{"a\r\nb", "LF", "CRLF", "a\r\nb"},
	}
	for _, c := range cases {
		out, _ := io.ReadAll(newEOLReader(strings.NewReader(c.in), c.from, c.to))
		if string(out) != c.want {
			t.Errorf("%q %s->%s = %q want %q", c.in, c.from, c.to, out, c.want)
		}
	}
}

func TestLocalTransfer(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "tree", "sub"), 0o755)
	big := bytes.Repeat([]byte("x"), 300*1024)
	os.WriteFile(filepath.Join(src, "tree", "big.bin"), big, 0o640)
	os.WriteFile(filepath.Join(src, "tree", "sub", "notes.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(src, "tree", "skip.tmp"), []byte("tmp"), 0o644)
	old := time.Date(2021, 3, 4, 5, 6, 7, 0, time.Local)
	os.Chtimes(filepath.Join(src, "tree", "big.bin"), old, old)

	// a partial .filepart from an interrupted transfer
	os.MkdirAll(filepath.Join(dst, "tree"), 0o755)
	os.WriteFile(filepath.Join(dst, "tree", "big.bin.filepart"), big[:100*1024], 0o640)

	m := NewManager(testRes{&localfs.FS{}}, nil, nil, 2)
	defer m.Close()
	ctx := context.Background()
	opts := Options{Mode: "auto", TextMask: "*.txt", PreserveTime: true, PreservePerms: true, Overwrite: "overwrite", ResumeThreshold: 1024,
		FileMask: "| *.tmp", SrcEOL: "LF", DstEOL: "CRLF", Parallel: 2}
	snap, err := m.Run(ctx, Request{Src: "local", Dst: "local", Paths: []string{filepath.Join(src, "tree")}, DstDir: dst, Options: opts})
	if err != nil {
		t.Fatalf("run: %v %+v", err, snap)
	}
	if snap.DoneFiles != 2 {
		t.Fatalf("done files = %d (%+v)", snap.DoneFiles, snap)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "tree", "big.bin"))
	if !bytes.Equal(got, big) {
		t.Fatalf("big.bin differs after resume (%d bytes)", len(got))
	}
	if _, err := os.Stat(filepath.Join(dst, "tree", "big.bin.filepart")); !os.IsNotExist(err) {
		t.Error(".filepart left behind")
	}
	st, _ := os.Stat(filepath.Join(dst, "tree", "big.bin"))
	if !st.ModTime().Equal(old) {
		t.Errorf("mtime not preserved: %v", st.ModTime())
	}
	if st.Mode().Perm() != 0o640 {
		t.Errorf("perms not preserved: %o", st.Mode().Perm())
	}
	txt, _ := os.ReadFile(filepath.Join(dst, "tree", "sub", "notes.txt"))
	if string(txt) != "one\r\ntwo\r\n" {
		t.Errorf("text mode conversion: %q", txt)
	}
	if _, err := os.Stat(filepath.Join(dst, "tree", "skip.tmp")); !os.IsNotExist(err) {
		t.Error("masked file was transferred")
	}

	// "newer only" skips unchanged files
	opts.NewerOnly = true
	snap, err = m.Run(ctx, Request{Src: "local", Dst: "local", Paths: []string{filepath.Join(src, "tree", "big.bin")}, DstDir: filepath.Join(dst, "tree"), Options: opts})
	if err != nil || snap.SkippedFiles != 1 {
		t.Fatalf("newer-only: %v skipped=%d", err, snap.SkippedFiles)
	}

	// move deletes the source
	opts.NewerOnly = false
	opts.Move = true
	os.WriteFile(filepath.Join(src, "mv.dat"), []byte("move me"), 0o644)
	if _, err := m.Run(ctx, Request{Src: "local", Dst: "local", Paths: []string{filepath.Join(src, "mv.dat")}, DstDir: dst, Options: opts}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "mv.dat")); !os.IsNotExist(err) {
		t.Error("source not removed by move")
	}
}
