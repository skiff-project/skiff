//go:build integration

package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testArchives makes and opens archives where the files are: on the SSH
// server with its tar (busybox) and unzip, and on this computer by Skiff.
func testArchives(t *testing.T, e *e2e) {
	t.Run("sftp", func(t *testing.T) {
		ci := e.connect(t, "sftp")
		root := e.remoteRoot(t, ci)
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": root + "/site"}, nil)
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": root + "/site/css"}, nil)
		e.put(t, ci.ID, root+"/site/index.html", "<h1>Accueil</h1>\n")
		e.put(t, ci.ID, root+"/site/css/style.css", "body{margin:0}\n")
		for _, f := range []string{"tar.gz", "tar"} {
			var p string
			e.must(t, "archive.compress", map[string]any{"conn": ci.ID, "dir": root, "names": []string{"site"}, "archive": "site." + f, "format": f}, &p)
			if p != root+"/site."+f || !e.exists(ci.ID, p) {
				t.Fatalf("%s archive %q", f, p)
			}
			dest := root + "/out-" + strings.ReplaceAll(f, ".", "")
			e.must(t, "archive.extract", map[string]any{"conn": ci.ID, "archive": p, "dest": dest}, nil)
			if got := e.readText(t, ci.ID, dest+"/site/css/style.css"); got != "body{margin:0}\n" {
				t.Errorf("%s: extracted %q", f, got)
			}
		}
		if err := e.mustFail(t, "archive.compress", map[string]any{"conn": ci.ID, "dir": root, "names": []string{"site"}, "archive": "site.tar", "format": "tar"}); !strings.Contains(err.Error(), "already exists") {
			t.Errorf("existing archive: %v", err)
		}
		// zip: busybox has unzip but no zip (python3 may stand in)
		err := e.call("archive.compress", map[string]any{"conn": ci.ID, "dir": root, "names": []string{"site"}, "archive": "site.zip", "format": "zip"}, nil)
		if err != nil && !strings.Contains(err.Error(), "neither zip nor python3") {
			t.Errorf("zip: %v", err)
		}
		// a zip made elsewhere, opened on the server
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("docs/lisez-moi.txt")
		w.Write([]byte("bonjour\n"))
		zw.Close()
		e.upload(t, ci.ID, root+"/docs.zip", buf.Bytes(), true)
		e.must(t, "archive.extract", map[string]any{"conn": ci.ID, "archive": root + "/docs.zip", "dest": root + "/docs-out"}, nil)
		if got := e.readText(t, ci.ID, root+"/docs-out/docs/lisez-moi.txt"); got != "bonjour\n" {
			t.Errorf("zip extracted %q", got)
		}
		// a member that would land outside the destination: refused
		buf.Reset()
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: "../evade.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
		tw.Write([]byte("x"))
		tw.Close()
		gz.Close()
		e.upload(t, ci.ID, root+"/evil.tar.gz", buf.Bytes(), true)
		// refused by Skiff, or brought back in the folder by the tar of the
		// server (busybox and GNU tar remove the leading "../")
		if err := e.call("archive.extract", map[string]any{"conn": ci.ID, "archive": root + "/evil.tar.gz", "dest": root + "/evil"}, nil); err != nil && !strings.Contains(err.Error(), "unsafe path") {
			t.Errorf("zip slip on the server: %v", err)
		} else if err == nil && !e.exists(ci.ID, root+"/evil/evade.txt") {
			t.Error("the member was not brought back in the folder")
		}
		if e.exists(ci.ID, root+"/evade.txt") {
			t.Error("a file was written outside the destination")
		}
		e.mustFail(t, "archive.extract", map[string]any{"conn": ci.ID, "archive": root + "/site/index.html", "dest": root})
		e.mustFail(t, "archive.compress", map[string]any{"conn": ci.ID, "dir": root, "names": []string{"../etc"}, "archive": "x.tar", "format": "tar"})
	})
	t.Run("local", func(t *testing.T) {
		root := filepath.Join(e.home, uniq("archives"))
		os.MkdirAll(filepath.Join(root, "site"), 0o755)
		os.WriteFile(filepath.Join(root, "site", "a.txt"), []byte("A\n"), 0o644)
		t.Cleanup(func() { os.RemoveAll(root) })
		var p string
		e.must(t, "archive.compress", map[string]any{"conn": "local", "dir": root, "names": []string{"site"}, "archive": "site.zip", "format": "zip"}, &p)
		e.must(t, "archive.extract", map[string]any{"conn": "local", "archive": p, "dest": filepath.Join(root, "x")}, nil)
		if b, _ := os.ReadFile(filepath.Join(root, "x", "site", "a.txt")); string(b) != "A\n" {
			t.Errorf("local zip %q", b)
		}
	})
	t.Run("ftpNeedsAShell", func(t *testing.T) {
		ci := e.connect(t, "ftp")
		if err := e.mustFail(t, "archive.compress", map[string]any{"conn": ci.ID, "dir": "/", "names": []string{"x"}, "archive": "x.tar", "format": "tar"}); !strings.Contains(err.Error(), "shell") {
			t.Errorf("ftp: %v", err)
		}
	})
}
