//go:build integration

package core

// Run with the Docker test servers up:
//   docker compose -f testenv/docker-compose.yml up -d
//   go test -tags integration ./internal/core/ -run TestProtocols -v

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/prompt"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
)

func testCore(t *testing.T) *Core {
	t.Helper()
	dir := t.TempDir()
	st, err := config.Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	auto := prompt.Func(func(ctx context.Context, req prompt.Request) (prompt.Response, error) {
		switch req.Kind {
		case prompt.HostKey, prompt.Certificate:
			return prompt.Response{Choice: "once"}, nil
		case prompt.Overwrite:
			return prompt.Response{Choice: "overwrite"}, nil
		}
		return prompt.Response{Choice: "cancel"}, prompt.ErrCancelled
	})
	return New(st, Options{Prompter: auto})
}

func sites() map[string]config.Site {
	base := func(proto, host string, port int, user, pass string) config.Site {
		s := config.NewSite()
		s.Protocol, s.Host, s.Port, s.User, s.Password = proto, host, port, user, pass
		s.UseAgent = false
		return s
	}
	ftps := base("ftp", "127.0.0.1", 2121, "skiff", "skiff")
	ftps.FTPS = "explicit"
	s3 := base("s3", "127.0.0.1", 9000, "skiffadmin", "skiffsecret")
	s3.S3PathStyle = true
	s3.S3Region = "us-east-1"
	return map[string]config.Site{
		"sftp":   base("sftp", "127.0.0.1", 2222, "skiff", "skiff"),
		"scp":    base("scp", "127.0.0.1", 2222, "skiff", "skiff"),
		"ftp":    base("ftp", "127.0.0.1", 2121, "skiff", "skiff"),
		"ftps":   ftps,
		"webdav": base("webdav", "127.0.0.1", 8080, "skiff", "skiff"),
		"s3":     s3,
	}
}

func TestProtocols(t *testing.T) {
	for name, site := range sites() {
		site := site
		t.Run(name, func(t *testing.T) {
			c := testCore(t)
			defer c.Shutdown()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			conn, err := c.Connect(ctx, "", &site, "")
			if err != nil {
				t.Fatalf("connect: %v", err)
			}
			fs, _ := conn.FS(ctx)
			caps := fs.Caps()
			home := conn.Info().Home
			t.Logf("home=%s caps=%+v", home, caps)
			root := vfs.Join("posix", home, fmt.Sprintf("skiff-test-%d", time.Now().UnixNano()))
			if site.Protocol == "s3" {
				root = "/skiff-test-" + fmt.Sprint(time.Now().UnixNano()%1000000)
			}
			if err := c.Mkdir(ctx, conn.ID, root, 0); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			defer c.Delete(context.Background(), conn.ID, []string{root}, false)

			// write + read text
			p := root + "/hello world.txt"
			if err := c.CreateFile(ctx, conn.ID, p, []byte("héllo\nworld\n")); err != nil {
				t.Fatalf("create: %v", err)
			}
			fc, err := c.ReadText(ctx, conn.ID, p, "")
			if err != nil || fc.Content != "héllo\nworld\n" {
				t.Fatalf("read: %v %q", err, fc.Content)
			}
			// list
			lr, err := c.List(ctx, conn.ID, root)
			if err != nil || len(lr.Entries) != 1 || lr.Entries[0].Name != "hello world.txt" || lr.Entries[0].Size != 13 {
				t.Fatalf("list: %v %+v", err, lr.Entries)
			}
			// subdir + rename
			if err := c.Mkdir(ctx, conn.ID, root+"/sub", 0); err != nil {
				t.Fatalf("mkdir sub: %v", err)
			}
			if err := c.Rename(ctx, conn.ID, p, root+"/sub/renamed.txt"); err != nil {
				t.Fatalf("rename: %v", err)
			}
			if _, err := c.Stat(ctx, conn.ID, root+"/sub/renamed.txt"); err != nil {
				t.Fatalf("stat renamed: %v", err)
			}
			if caps.Chmod {
				m := uint32(0o600)
				if err := c.SetAttrs(ctx, conn.ID, []string{root + "/sub/renamed.txt"}, Attrs{Mode: &m}); err != nil {
					// the pure-ftpd test container runs with -R (chmod disabled)
					t.Logf("chmod: %v", err)
				} else if e, _ := c.Stat(ctx, conn.ID, root+"/sub/renamed.txt"); e.Mode&0o777 != 0o600 && site.Protocol != "ftp" {
					t.Errorf("chmod not applied: %o", e.Mode)
				}
			}
			if caps.SetMtime {
				mt := time.Date(2020, 5, 17, 10, 30, 0, 0, time.UTC)
				if err := c.SetAttrs(ctx, conn.ID, []string{root + "/sub/renamed.txt"}, Attrs{MTime: &mt}); err != nil {
					t.Errorf("chtimes: %v", err)
				} else if e, _ := c.Stat(ctx, conn.ID, root+"/sub/renamed.txt"); !e.ModTime.Equal(mt) {
					t.Errorf("mtime = %v, want %v", e.ModTime, mt)
				}
			}

			// transfer queue: upload a local tree, download it back
			local := t.TempDir()
			os.MkdirAll(filepath.Join(local, "tree", "deep", "er"), 0o755)
			big := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
			os.WriteFile(filepath.Join(local, "tree", "big.bin"), big, 0o644)
			os.WriteFile(filepath.Join(local, "tree", "deep", "a.txt"), []byte("A"), 0o644)
			os.WriteFile(filepath.Join(local, "tree", "deep", "er", "b.txt"), []byte("BB"), 0o644)
			opts := c.DefaultTransferOptions(LocalID, conn.ID)
			opts.Overwrite = "overwrite"
			opts.ContinueOnError = true
			snap, err := c.Transfers.Run(ctx, transfer.Request{Src: LocalID, Dst: conn.ID, Paths: []string{filepath.Join(local, "tree")}, DstDir: root, Options: opts})
			if err != nil || snap.DoneFiles != 3 {
				t.Fatalf("upload: %v %+v", err, snap)
			}
			e, err := c.Stat(ctx, conn.ID, root+"/tree/big.bin")
			if err != nil || e.Size != int64(len(big)) {
				t.Fatalf("uploaded big: %v %+v", err, e)
			}
			dl := t.TempDir()
			snap, err = c.Transfers.Run(ctx, transfer.Request{Src: conn.ID, Dst: LocalID, Paths: []string{root + "/tree"}, DstDir: dl, Options: opts})
			if err != nil || snap.DoneFiles != 3 {
				t.Fatalf("download: %v %+v", err, snap)
			}
			got, _ := os.ReadFile(filepath.Join(dl, "tree", "big.bin"))
			if !bytes.Equal(got, big) {
				t.Fatalf("downloaded content differs (%d bytes)", len(got))
			}
			if b, _ := os.ReadFile(filepath.Join(dl, "tree", "deep", "er", "b.txt")); string(b) != "BB" {
				t.Fatalf("deep file wrong: %q", b)
			}
			// resume download from a partial file
			if caps.ResumeDownload {
				r, err := fs.Open(ctx, root+"/tree/big.bin", 1000)
				if err != nil {
					t.Fatalf("open at offset: %v", err)
				}
				rest, _ := io.ReadAll(r)
				r.Close()
				if !bytes.Equal(rest, big[1000:]) {
					t.Fatalf("offset read wrong (%d bytes)", len(rest))
				}
			}
			// sync preview: change a local file and compare
			time.Sleep(1100 * time.Millisecond)
			os.WriteFile(filepath.Join(local, "tree", "deep", "a.txt"), []byte("AAA"), 0o644)
			os.WriteFile(filepath.Join(local, "tree", "new.txt"), []byte("new"), 0o644)
			plan, err := c.SyncCompare(ctx, SyncRequest{LocalDir: filepath.Join(local, "tree"), RemoteConn: conn.ID, RemoteDir: root + "/tree", Direction: "remote", Criteria: "either"})
			if err != nil {
				t.Fatalf("sync compare: %v", err)
			}
			var acts []string
			for _, a := range plan.Actions {
				acts = append(acts, a.Action+":"+a.Rel)
			}
			want := []string{"upload:deep/a.txt", "upload:new.txt"}
			if strings.Join(acts, ",") != strings.Join(want, ",") {
				t.Errorf("sync plan = %v, want %v", acts, want)
			}
			// recursive delete
			if err := c.Delete(ctx, conn.ID, []string{root + "/tree"}, false); err != nil {
				t.Fatalf("delete tree: %v", err)
			}
			if _, err := c.Stat(ctx, conn.ID, root+"/tree"); err == nil {
				t.Fatalf("tree still exists")
			}
		})
	}
}
