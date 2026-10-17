//go:build integration

package server

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/core"
	"skiff/internal/prompt"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
)

// target is a connection with a scratch directory.
type target struct {
	id, proto string
	caps      vfs.Caps
	root      string
}

func (e *e2e) localTarget(t *testing.T) target {
	t.Helper()
	var info struct {
		LocalCaps vfs.Caps `json:"localCaps"`
	}
	e.must(t, "app.info", nil, &info)
	root := filepath.Join(e.home, uniq("local"))
	e.must(t, "fs.mkdir", map[string]any{"conn": "local", "path": root}, nil)
	return target{id: "local", proto: "local", caps: info.LocalCaps, root: root}
}

func (e *e2e) remoteTarget(t *testing.T, proto string) target {
	t.Helper()
	ci := e.connect(t, proto)
	return target{id: ci.ID, proto: proto, caps: ci.Caps, root: e.remoteRoot(t, ci)}
}

const helloSHA256 = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"
const helloMD5 = "b1946ac92492d2347c6235b4d2611184"

func testProtocols(t *testing.T, e *e2e) {
	t.Run("local", func(t *testing.T) { fileOps(t, e, e.localTarget(t)) })
	for _, proto := range []string{"sftp", "scp", "ftp", "webdav", "s3"} {
		proto := proto
		t.Run(proto, func(t *testing.T) { fileOps(t, e, e.remoteTarget(t, proto)) })
	}
	t.Run("ftp-plain", func(t *testing.T) {
		s := testSite("ftp")
		s.FTPS = "none"
		ci := e.open(t, e.saveSite(t, s, "skiff").ID)
		fileOps(t, e, target{id: ci.ID, proto: "ftp", caps: ci.Caps, root: e.remoteRoot(t, ci)})
	})
	t.Run("ftps-implicit", func(t *testing.T) {
		s := testSite("ftp")
		s.Port, s.FTPS = 9990, "implicit"
		ci := e.open(t, e.saveSite(t, s, "skiff").ID)
		fileOps(t, e, target{id: ci.ID, proto: "ftp", caps: ci.Caps, root: e.remoteRoot(t, ci)})
	})
	t.Run("ftp-active", func(t *testing.T) {
		// active mode needs the server to connect back: only check listing works or fails cleanly
		s := testSite("ftp")
		s.FTPS, s.FTPActive = "none", true
		var ci core.ConnInfo
		if err := e.call("conn.open", map[string]any{"site": s, "password": "skiff"}, &ci); err != nil {
			t.Skipf("active FTP unavailable from a container: %v", err)
		}
		if err := e.call("fs.list", map[string]string{"conn": ci.ID, "path": ci.Home}, nil); err != nil {
			t.Logf("active FTP listing (expected to fail behind Docker NAT): %v", err)
		}
	})
	t.Run("remoteRecycleBin", func(t *testing.T) {
		s := testSite("sftp")
		s.RecycleBin = "/config/" + uniq(".corbeille")
		ci := e.open(t, e.saveSite(t, s, "skiff").ID)
		root := e.remoteRoot(t, ci)
		e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": root + "/jeter.txt", "content": "x"}, nil)
		e.must(t, "fs.delete", map[string]any{"conn": ci.ID, "paths": []string{root + "/jeter.txt"}, "recycle": true}, nil)
		if e.exists(ci.ID, root+"/jeter.txt") || !e.exists(ci.ID, s.RecycleBin+"/jeter.txt") {
			t.Fatal("file not moved to the remote recycle bin")
		}
		e.call("fs.delete", map[string]any{"conn": ci.ID, "paths": []string{s.RecycleBin}}, nil)
	})
	t.Run("localFolderWatch", func(t *testing.T) {
		lt := e.localTarget(t)
		other := e.localTarget(t)
		e.must(t, "fs.watchLocal", map[string]any{"dirs": []string{lt.root}}, nil)
		seen := func(dir string) bool {
			e.mu.Lock()
			defer e.mu.Unlock()
			for _, c := range e.changed {
				if c == "local|"+dir {
					return true
				}
			}
			return false
		}
		os.WriteFile(filepath.Join(lt.root, "externe.txt"), []byte("écrit hors de Skiff"), 0o644)
		eventually(t, "fs.changed for the watched folder", 5*time.Second, func() bool { return seen(lt.root) })
		os.WriteFile(filepath.Join(other.root, "ignore.txt"), []byte("x"), 0o644)
		time.Sleep(800 * time.Millisecond)
		if seen(other.root) {
			t.Error("an unwatched folder produced fs.changed")
		}
		e.must(t, "fs.watchLocal", map[string]any{"dirs": []string{}}, nil)
	})
	t.Run("localTrashAndOpeners", func(t *testing.T) {
		lt := e.localTarget(t)
		p := filepath.Join(lt.root, "corbeille.txt")
		e.must(t, "fs.createFile", map[string]any{"conn": "local", "path": p, "content": "x"}, nil)
		e.must(t, "fs.delete", map[string]any{"conn": "local", "paths": []string{p}, "recycle": true}, nil)
		if _, err := os.Stat(filepath.Join(e.home, ".local/share/Trash/files/corbeille.txt")); err != nil {
			t.Fatalf("not in the system trash: %v", err)
		}
		// open with the default application, reveal in the file manager
		os.Remove(e.opened)
		f := filepath.Join(lt.root, "ouvrir.txt")
		os.WriteFile(f, []byte("x"), 0o644)
		e.must(t, "fs.open", map[string]string{"conn": "local", "path": f}, nil)
		e.must(t, "fs.reveal", map[string]string{"conn": "local", "path": f}, nil)
		ci := e.connect(t, "sftp")
		root := e.remoteRoot(t, ci)
		e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": root + "/distant.txt", "content": "x"}, nil)
		e.must(t, "fs.open", map[string]string{"conn": ci.ID, "path": root + "/distant.txt"}, nil)
		eventually(t, "the opener to run", 5*time.Second, func() bool {
			b, _ := os.ReadFile(e.opened)
			return strings.Count(string(b), "\n") >= 3
		})
		b, _ := os.ReadFile(e.opened)
		if !strings.Contains(string(b), f) || !strings.Contains(string(b), lt.root) || !strings.Contains(string(b), "distant.txt") {
			t.Errorf("xdg-open calls: %s", b)
		}
	})
}

// fileOps exercises every file operation a protocol supports.
func fileOps(t *testing.T, e *e2e, tg target) {
	id, root, caps := tg.id, tg.root, tg.caps
	p := func(name string) string { return root + "/" + name }
	if tg.proto == "local" {
		p = func(name string) string { return filepath.Join(root, name) }
	}
	// create, list, stat, read
	e.must(t, "fs.createFile", map[string]any{"conn": id, "path": p("a.txt"), "content": "hello\n"}, nil)
	var lr core.ListResult
	e.must(t, "fs.list", map[string]string{"conn": id, "path": root}, &lr)
	if len(lr.Entries) != 1 || lr.Entries[0].Name != "a.txt" || lr.Entries[0].Size != 6 {
		t.Fatalf("list = %+v", lr.Entries)
	}
	var st vfs.Entry
	e.must(t, "fs.stat", map[string]string{"conn": id, "path": p("a.txt")}, &st)
	if st.Size != 6 || st.IsDir {
		t.Fatalf("stat = %+v", st)
	}
	if got := e.readText(t, id, p("a.txt")); got != "hello\n" {
		t.Fatalf("readText = %q", got)
	}
	// editor saves: conflict detection, encodings, BOM, line endings
	var fc core.FileContent
	e.must(t, "fs.readText", map[string]string{"conn": id, "path": p("a.txt")}, &fc)
	stale := fc.MTime.Add(-time.Hour)
	if err := e.call("fs.writeText", core.WriteText{Conn: id, Path: p("a.txt"), Content: "x", Encoding: "utf-8", EOL: "LF", Expect: &stale}, nil); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Errorf("stale save must be refused: %v", err)
	}
	e.must(t, "fs.writeText", core.WriteText{Conn: id, Path: p("a.txt"), Content: "x", Encoding: "utf-8", EOL: "LF", Expect: &stale, Force: true}, nil)
	e.must(t, "fs.writeText", core.WriteText{Conn: id, Path: p("a.txt"), Content: "hello\n", Encoding: "utf-8", EOL: "LF", Expect: &fc.MTime, Force: true}, nil)
	e.must(t, "fs.writeText", core.WriteText{Conn: id, Path: p("w1252.txt"), Content: "café\nthé\n", Encoding: "windows-1252", EOL: "CRLF"}, nil)
	if raw := e.download(t, id, p("w1252.txt")); !bytes.Equal(raw, []byte("caf\xe9\r\nth\xe9\r\n")) {
		t.Errorf("windows-1252 + CRLF bytes = %q", raw)
	}
	e.must(t, "fs.readText", map[string]string{"conn": id, "path": p("w1252.txt")}, &fc)
	if fc.Content != "café\nthé\n" || fc.Encoding != "windows-1252" || fc.EOL != "CRLF" {
		t.Errorf("readText windows-1252 = %+v", fc)
	}
	e.must(t, "fs.writeText", core.WriteText{Conn: id, Path: p("bom.txt"), Content: "é", Encoding: "utf-8", BOM: true, EOL: "LF"}, nil)
	if raw := e.download(t, id, p("bom.txt")); !bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		t.Errorf("BOM missing: %q", raw)
	}
	e.must(t, "fs.readText", map[string]string{"conn": id, "path": p("bom.txt")}, &fc)
	if !fc.BOM || fc.Content != "é" {
		t.Errorf("BOM read = %+v", fc)
	}
	// folders, rename, move
	e.must(t, "fs.mkdir", map[string]any{"conn": id, "path": p("sub")}, nil)
	e.must(t, "fs.rename", map[string]string{"conn": id, "from": p("a.txt"), "to": p("sub/b.txt")}, nil)
	if e.exists(id, p("a.txt")) || e.readText(t, id, p("sub/b.txt")) != "hello\n" {
		t.Fatal("rename failed")
	}
	e.must(t, "fs.move", map[string]any{"conn": id, "paths": []string{p("sub/b.txt")}, "dstDir": root}, nil)
	if !e.exists(id, p("b.txt")) || e.exists(id, p("sub/b.txt")) {
		t.Fatalf("move failed: stat b.txt: %v; stat sub/b.txt: %v", e.call("fs.stat", map[string]string{"conn": id, "path": p("b.txt")}, nil), e.call("fs.stat", map[string]string{"conn": id, "path": p("sub/b.txt")}, nil))
	}
	if caps.ServerCopy {
		var dup string
		e.must(t, "fs.duplicate", map[string]any{"conn": id, "paths": []string{p("b.txt")}, "dstDir": p("sub")}, &dup)
		if e.readText(t, id, p("sub/b.txt")) != "hello\n" {
			t.Fatal("server-side copy failed")
		}
		e.must(t, "fs.duplicate", map[string]any{"conn": id, "paths": []string{p("b.txt")}, "dstDir": root, "names": map[string]string{p("b.txt"): "copie.txt"}}, &dup)
		if !e.exists(id, p("copie.txt")) {
			t.Fatal("server-side copy with a new name failed")
		}
	}
	// attributes
	if caps.Chmod {
		m := uint32(0o640)
		err := e.call("fs.setAttrs", map[string]any{"conn": id, "paths": []string{p("b.txt")}, "attrs": core.Attrs{Mode: &m}}, nil)
		if tg.proto == "ftp" {
			t.Logf("FTP chmod (pure-ftpd may refuse SITE CHMOD): %v", err)
		} else if err != nil {
			t.Fatal(err)
		} else if e.must(t, "fs.stat", map[string]string{"conn": id, "path": p("b.txt")}, &st); st.Mode&0o777 != 0o640 {
			t.Errorf("chmod: %o", st.Mode)
		}
		if tg.proto != "ftp" {
			m := uint32(0o600)
			e.must(t, "fs.setAttrs", map[string]any{"conn": id, "paths": []string{p("sub")}, "attrs": core.Attrs{Mode: &m, Recursive: true, AddXToDirs: true}}, nil)
			var sd vfs.Entry
			e.must(t, "fs.stat", map[string]string{"conn": id, "path": p("sub")}, &sd)
			e.must(t, "fs.stat", map[string]string{"conn": id, "path": p("sub/b.txt")}, &st)
			if sd.Mode&0o777 != 0o700 || (caps.ServerCopy && st.Mode&0o777 != 0o600) {
				t.Errorf("recursive chmod with +X: dir %o file %o", sd.Mode&0o777, st.Mode&0o777)
			}
			m = 0o755
			e.must(t, "fs.setAttrs", map[string]any{"conn": id, "paths": []string{p("sub")}, "attrs": core.Attrs{Mode: &m}}, nil)
		}
	}
	if caps.SetMtime {
		mt := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
		e.must(t, "fs.setAttrs", map[string]any{"conn": id, "paths": []string{p("b.txt")}, "attrs": core.Attrs{MTime: &mt}}, nil)
		e.must(t, "fs.stat", map[string]string{"conn": id, "path": p("b.txt")}, &st)
		if !st.ModTime.Equal(mt) && st.ModTime.Truncate(time.Minute) != mt.Truncate(time.Minute) {
			t.Errorf("mtime = %v", st.ModTime)
		}
	}
	if caps.Chown {
		e.must(t, "fs.stat", map[string]string{"conn": id, "path": p("b.txt")}, &st)
		e.must(t, "fs.setAttrs", map[string]any{"conn": id, "paths": []string{p("b.txt")}, "attrs": core.Attrs{Owner: st.Owner, Group: st.Group}}, nil)
	}
	if caps.Owners {
		var owners map[string][]string
		e.must(t, "fs.owners", map[string]string{"conn": id}, &owners)
		if tg.proto == "sftp" && len(owners["users"]) == 0 {
			t.Errorf("owners = %v", owners)
		}
	}
	// links
	if caps.Symlink {
		e.must(t, "fs.link", map[string]any{"conn": id, "target": "b.txt", "link": p("lien")}, nil)
		e.must(t, "fs.list", map[string]string{"conn": id, "path": root}, &lr)
		ok := false
		for _, en := range lr.Entries {
			ok = ok || (en.Name == "lien" && en.IsLink)
		}
		if !ok {
			t.Errorf("symlink not listed as a link: %+v", lr.Entries)
		}
	}
	if caps.Hardlink {
		e.must(t, "fs.link", map[string]any{"conn": id, "target": p("b.txt"), "link": p("physique"), "hard": true}, nil)
		if e.readText(t, id, p("physique")) != "hello\n" {
			t.Error("hard link")
		}
	}
	// size, checksum, space
	var size core.SizeResult
	e.must(t, "fs.size", map[string]any{"conn": id, "paths": []string{root}}, &size)
	if size.Files < 3 || size.Bytes == 0 {
		t.Errorf("size = %+v", size)
	}
	if caps.Checksum {
		algo, want := "sha256", helloSHA256
		if tg.proto == "s3" {
			algo, want = "md5", helloMD5
		}
		if tg.proto == "ftp" {
			algo = ""
		}
		var sum string
		if err := e.call("fs.checksum", map[string]string{"conn": id, "path": p("b.txt"), "algo": algo}, &sum); err != nil {
			t.Errorf("checksum: %v", err)
		} else if algo != "" && sum != want {
			t.Errorf("checksum = %s", sum)
		}
		if tg.proto != "s3" && tg.proto != "ftp" {
			for _, a := range []string{"md5", "sha1", "sha512"} {
				e.must(t, "fs.checksum", map[string]string{"conn": id, "path": p("b.txt"), "algo": a}, &sum)
				if a == "md5" && sum != helloMD5 {
					t.Errorf("md5 = %s", sum)
				}
			}
		}
	}
	if caps.Space {
		var sp core.SpaceResult
		err := e.call("fs.space", map[string]string{"conn": id, "path": root}, &sp)
		switch {
		case err != nil && tg.proto == "webdav" && strings.Contains(err.Error(), "unsupported"):
			t.Logf("WebDAV server without quota properties: %v", err)
		case err != nil:
			t.Error(err)
		case sp.Total <= 0:
			t.Errorf("space = %+v", sp)
		}
	} else if err := e.call("fs.space", map[string]string{"conn": id, "path": root}, nil); err == nil {
		t.Errorf("fs.space must fail when unsupported")
	}
	if caps.Exec {
		var res core.ExecResult
		if tg.proto == "ftp" {
			e.must(t, "fs.exec", map[string]string{"conn": id, "command": "NOOP", "cwd": root}, &res)
			if !strings.HasPrefix(res.Stdout, "200") {
				t.Errorf("FTP raw command: %+v", res)
			}
		} else {
			e.must(t, "fs.exec", map[string]string{"conn": id, "command": "pwd; echo erreur >&2; exit 3", "cwd": root}, &res)
			if strings.TrimSpace(res.Stdout) != strings.TrimRight(root, "/") || strings.TrimSpace(res.Stderr) != "erreur" || res.Code != 3 {
				t.Errorf("exec = %+v", res)
			}
		}
	} else if err := e.call("fs.exec", map[string]string{"conn": id, "command": "echo", "cwd": root}, nil); err == nil {
		t.Errorf("fs.exec must fail without exec support")
	}
	// browser upload/download endpoints
	data := make([]byte, 300<<10)
	rand.Read(data)
	if code := e.upload(t, id, p("up/deep/data.bin"), data, false); code >= 300 {
		t.Fatalf("upload: HTTP %d", code)
	}
	if code := e.upload(t, id, p("up/deep/data.bin"), data, false); code != 409 {
		t.Errorf("upload over an existing file without overwrite: HTTP %d", code)
	}
	if code := e.upload(t, id, p("up/deep/data.bin"), data[:1000], true); code >= 300 {
		t.Errorf("upload with overwrite: HTTP %d", code)
	}
	if got := e.download(t, id, p("up/deep/data.bin")); !bytes.Equal(got, data[:1000]) {
		t.Error("download differs from upload")
	}
	// delete (recursive)
	e.must(t, "fs.delete", map[string]any{"conn": id, "paths": []string{p("up"), p("b.txt")}}, nil)
	if e.exists(id, p("up")) || e.exists(id, p("b.txt")) {
		t.Fatal("delete failed")
	}
}

// ---- transfers ----

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		f := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func testTransfers(t *testing.T, e *e2e) {
	s := testSite("sftp")
	s.EOL = "CRLF" // text mode converts to CRLF on this server
	ci := e.open(t, e.saveSite(t, s, "skiff").ID)
	root := e.remoteRoot(t, ci)
	local := filepath.Join(e.home, uniq("transfer"))
	writeTree(t, local, map[string]string{
		"site/index.html":     "<h1>accueil</h1>\n",
		"site/css/style.css":  "body{}\n",
		"site/.htaccess":      "deny\n",
		"site/Notes.TXT":      "une\ndeux\n",
		"site/brouillon.tmp":  "tmp",
		"site/node_modules/x": "x",
	})
	old := time.Date(2022, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(filepath.Join(local, "site/index.html"), old, old)

	t.Run("uploadDownloadTree", func(t *testing.T) {
		o := e.defaults(t, "local", ci.ID)
		j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "site")}, DstDir: root, Options: o})
		if j.Status != transfer.Done || j.TotalFiles != 6 {
			t.Fatalf("upload job = %+v", j)
		}
		var st vfs.Entry
		e.must(t, "fs.stat", map[string]string{"conn": ci.ID, "path": root + "/site/index.html"}, &st)
		if !st.ModTime.Equal(old) {
			t.Errorf("preserved time = %v, want %v", st.ModTime, old)
		}
		back := filepath.Join(local, "back")
		j = e.enqueue(t, transfer.Request{Src: ci.ID, Dst: "local", Paths: []string{root + "/site"}, DstDir: back, Options: e.defaults(t, ci.ID, "local")})
		if j.Status != transfer.Done {
			t.Fatalf("download job = %+v", j)
		}
		b, _ := os.ReadFile(filepath.Join(back, "site/css/style.css"))
		if string(b) != "body{}\n" {
			t.Errorf("downloaded content = %q", b)
		}
		if fi, _ := os.Stat(filepath.Join(back, "site/index.html")); fi == nil || !fi.ModTime().Equal(old) {
			t.Errorf("download did not preserve the date")
		}
	})
	t.Run("masksHiddenCase", func(t *testing.T) {
		o := e.defaults(t, "local", ci.ID)
		o.FileMask, o.ExcludeHidden, o.CaseMode = "* | *.tmp; node_modules/", true, "lower"
		dst := root + "/filtre"
		e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "site")}, DstDir: dst, Options: o})
		for name, want := range map[string]bool{"site/index.html": true, "site/notes.txt": true, "site/Notes.TXT": false, "site/brouillon.tmp": false, "site/node_modules": false, "site/.htaccess": false} {
			if e.exists(ci.ID, dst+"/"+name) != want {
				t.Errorf("%s exists = %v, want %v", name, !want, want)
			}
		}
	})
	t.Run("textMode", func(t *testing.T) {
		for _, mode := range []string{"text", "auto"} {
			o := e.defaults(t, "local", ci.ID)
			o.Mode = mode
			dst := root + "/" + mode
			e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "site/Notes.TXT")}, DstDir: dst, Options: o})
			raw := e.download(t, ci.ID, dst+"/Notes.TXT")
			if mode == "text" && string(raw) != "une\r\ndeux\r\n" {
				t.Errorf("text mode upload = %q", raw)
			}
			if mode == "auto" && string(raw) != "une\r\ndeux\r\n" { // *.txt is in the default text mask (case-insensitive)
				t.Errorf("auto mode upload = %q", raw)
			}
		}
		// back to LF on download
		o := e.defaults(t, ci.ID, "local")
		o.Mode = "text"
		dl := filepath.Join(local, "text-back")
		e.enqueue(t, transfer.Request{Src: ci.ID, Dst: "local", Paths: []string{root + "/text/Notes.TXT"}, DstDir: dl, Options: o})
		if b, _ := os.ReadFile(filepath.Join(dl, "Notes.TXT")); string(b) != "une\ndeux\n" {
			t.Errorf("text mode download = %q", b)
		}
	})
	t.Run("permissions", func(t *testing.T) {
		o := e.defaults(t, "local", ci.ID)
		o.PreservePerms, o.SetPerms, o.Perms = false, true, 0o604
		dst := root + "/perms"
		e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "site/css")}, DstDir: dst, Options: o})
		var st vfs.Entry
		e.must(t, "fs.stat", map[string]string{"conn": ci.ID, "path": dst + "/css/style.css"}, &st)
		if st.Mode&0o777 != 0o604 {
			t.Errorf("set permissions: %o", st.Mode&0o777)
		}
		e.must(t, "fs.stat", map[string]string{"conn": ci.ID, "path": dst + "/css"}, &st)
		if st.Mode&0o777 != 0o705 {
			t.Errorf("directory with +X: %o", st.Mode&0o777)
		}
	})
	t.Run("overwritePolicies", func(t *testing.T) {
		dir := root + "/ow"
		src := filepath.Join(local, "ow")
		writeTree(t, src, map[string]string{"x.txt": "NOUVEAU-CONTENU"})
		reset := func(content string, mt time.Time) {
			e.put(t, ci.ID, dir+"/x.txt", content)
			e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{dir + "/x.txt"}, "attrs": core.Attrs{MTime: &mt}}, nil)
		}
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": dir}, nil)
		up := func(policy string) transfer.Snapshot {
			o := e.defaults(t, "local", ci.ID)
			o.Overwrite = policy
			return e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(src, "x.txt")}, DstDir: dir, Options: o})
		}
		past := time.Now().Add(-48 * time.Hour)
		future := time.Now().Add(48 * time.Hour)
		reset("ANCIEN", past)
		if j := up("skip"); j.SkippedFiles != 1 || e.readText(t, ci.ID, dir+"/x.txt") != "ANCIEN" {
			t.Errorf("skip: %+v", j)
		}
		reset("PLUS-RECENT", future)
		if up("newer"); e.readText(t, ci.ID, dir+"/x.txt") != "PLUS-RECENT" {
			t.Error("newer must keep a more recent target")
		}
		reset("ANCIEN", past)
		if up("newer"); e.readText(t, ci.ID, dir+"/x.txt") != "NOUVEAU-CONTENU" {
			t.Error("newer must replace an older target")
		}
		reset("MEME-TAILLE-ABCD", past) // 16 bytes vs 15
		if up("differentSize"); e.readText(t, ci.ID, dir+"/x.txt") != "NOUVEAU-CONTENU" {
			t.Error("differentSize must replace")
		}
		reset("XXXXXXXXXXXXXXX", past) // same size
		if up("differentSize"); e.readText(t, ci.ID, dir+"/x.txt") != "XXXXXXXXXXXXXXX" {
			t.Error("differentSize must skip a same-size file")
		}
		if up("rename"); e.readText(t, ci.ID, dir+"/x (1).txt") != "NOUVEAU-CONTENU" {
			t.Error("rename (keep both)")
		}
		reset("NOUVEAU", past) // a prefix of the source
		if up("resume"); e.readText(t, ci.ID, dir+"/x.txt") != "NOUVEAU-CONTENU" {
			t.Errorf("resume = %q", e.readText(t, ci.ID, dir+"/x.txt"))
		}
		reset("ANCIEN", past)
		e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
			if ev.Kind == prompt.Overwrite {
				return prompt.Response{Choice: "overwrite", ApplyAll: true}, true
			}
			return defaultPolicy(ev)
		})
		before := len(e.promptsOf(prompt.Overwrite))
		up("ask")
		e.setPolicy(defaultPolicy)
		asked := e.promptsOf(prompt.Overwrite)
		if len(asked) != before+1 || asked[len(asked)-1].Details["target"] != dir+"/x.txt" || e.readText(t, ci.ID, dir+"/x.txt") != "NOUVEAU-CONTENU" {
			t.Errorf("ask: %d prompts, content %q", len(asked)-before, e.readText(t, ci.ID, dir+"/x.txt"))
		}
	})
	t.Run("filepartResumeVerifySpeedMove", func(t *testing.T) {
		big := make([]byte, 2<<20)
		rand.Read(big)
		os.WriteFile(filepath.Join(local, "gros.bin"), big, 0o644)
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": root + "/gros"}, nil)
		e.upload(t, ci.ID, root+"/gros/gros.bin.filepart", big[:700<<10], true)
		o := e.defaults(t, "local", ci.ID)
		o.Verify = true
		j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "gros.bin")}, DstDir: root + "/gros", Options: o})
		if j.Status != transfer.Done || !bytes.Equal(e.download(t, ci.ID, root+"/gros/gros.bin"), big) || e.exists(ci.ID, root+"/gros/gros.bin.filepart") {
			t.Fatalf("filepart resume + verify: %+v", j)
		}
		// speed limit: 768 KiB at 256 KiB/s
		os.WriteFile(filepath.Join(local, "lent.bin"), big[:768<<10], 0o644)
		o = e.defaults(t, "local", ci.ID)
		o.SpeedLimit = 256
		start := time.Now()
		e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "lent.bin")}, DstDir: root, Options: o})
		if d := time.Since(start); d < 2*time.Second {
			t.Errorf("speed limit not applied: %v", d)
		}
		// move: the source disappears
		o = e.defaults(t, "local", ci.ID)
		o.Move = true
		e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "lent.bin")}, DstDir: root + "/gros", Options: o})
		if _, err := os.Stat(filepath.Join(local, "lent.bin")); err == nil || !e.exists(ci.ID, root+"/gros/lent.bin") {
			t.Error("move did not delete the source")
		}
		// newer only: nothing to do the second time
		o = e.defaults(t, "local", ci.ID)
		o.NewerOnly = true
		if j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "gros.bin")}, DstDir: root + "/gros", Options: o}); j.SkippedFiles != 1 {
			t.Errorf("newerOnly: %+v", j)
		}
	})
	t.Run("errorsAndRetry", func(t *testing.T) {
		ro := root + "/lecture-seule"
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": ro}, nil)
		m := uint32(0o555)
		e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{ro}, "attrs": core.Attrs{Mode: &m}}, nil)
		e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
			if ev.Kind == prompt.Error {
				return prompt.Response{Choice: "skip"}, true
			}
			return defaultPolicy(ev)
		})
		o := e.defaults(t, "local", ci.ID)
		j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "site/css/style.css")}, DstDir: ro, Options: o})
		e.setPolicy(defaultPolicy)
		if j.Status != transfer.Partial || j.FailedFiles != 1 || len(e.promptsOf(prompt.Error)) == 0 {
			t.Fatalf("error job = %+v", j)
		}
		if ep := e.promptsOf(prompt.Error); ep[len(ep)-1].Details["conn"] != "local" {
			t.Errorf("error prompt details = %v", ep[len(ep)-1].Details)
		}
		m = 0o755
		e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{ro}, "attrs": core.Attrs{Mode: &m}}, nil)
		var retry string
		e.must(t, "transfer.retry", map[string]string{"id": j.ID}, &retry)
		if r := e.waitJob(t, retry); r.Status != transfer.Done || !e.exists(ci.ID, ro+"/style.css") {
			t.Fatalf("retry = %+v", r)
		}
		// continue on error: no question asked
		m = 0o555
		e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{ro}, "attrs": core.Attrs{Mode: &m}}, nil)
		n := len(e.promptsOf(prompt.Error))
		o.ContinueOnError = true
		j = e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "site/index.html")}, DstDir: ro, Options: o})
		if j.Status != transfer.Partial || len(e.promptsOf(prompt.Error)) != n {
			t.Errorf("continueOnError: %+v", j)
		}
		m = 0o755
		e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{ro}, "attrs": core.Attrs{Mode: &m}}, nil)
	})
}

func testQueue(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	local := filepath.Join(e.home, uniq("queue"))
	os.MkdirAll(local, 0o755)
	data := make([]byte, 600<<10)
	rand.Read(data)
	for _, n := range []string{"1.bin", "2.bin", "3.bin"} {
		os.WriteFile(filepath.Join(local, n), data, 0o644)
	}
	var prefs map[string]any
	e.must(t, "prefs.get", nil, &prefs)
	q := prefs["queue"].(map[string]any)
	q["maxParallel"] = 1
	e.must(t, "prefs.set", prefs, nil)
	defer func() { q["maxParallel"] = 3; e.must(t, "prefs.set", prefs, nil) }()
	var ids []string
	for _, n := range []string{"1.bin", "2.bin", "3.bin"} {
		o := e.defaults(t, "local", ci.ID)
		o.SpeedLimit = 200
		var id string
		e.must(t, "transfer.enqueue", transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, n)}, DstDir: root, Options: o}, &id)
		ids = append(ids, id)
	}
	status := func() map[string]transfer.Snapshot {
		var jobs []transfer.Snapshot
		e.must(t, "transfer.list", nil, &jobs)
		m := map[string]transfer.Snapshot{}
		for _, j := range jobs {
			m[j.ID] = j
		}
		return m
	}
	eventually(t, "the first job to run", 5*time.Second, func() bool { return status()[ids[0]].Status == transfer.Running })
	if s := status(); s[ids[1]].Status != transfer.Queued || s[ids[2]].Status != transfer.Queued {
		t.Fatalf("with one slot, jobs 2 and 3 must wait: %s %s", s[ids[1]].Status, s[ids[2]].Status)
	}
	e.must(t, "transfer.move", map[string]any{"id": ids[2], "delta": -1}, nil)
	var jobs []transfer.Snapshot
	e.must(t, "transfer.list", nil, &jobs)
	pos := map[string]int{}
	for i, j := range jobs {
		pos[j.ID] = i
	}
	if pos[ids[2]] > pos[ids[1]] {
		t.Errorf("move up did not reorder the queue")
	}
	e.must(t, "transfer.pause", map[string]any{"id": ids[0], "value": true}, nil)
	eventually(t, "pause", 5*time.Second, func() bool { return status()[ids[0]].Status == transfer.Paused })
	done := status()[ids[0]].DoneBytes
	time.Sleep(1500 * time.Millisecond)
	if status()[ids[0]].DoneBytes > done+64<<10 {
		t.Errorf("a paused job keeps transferring")
	}
	e.must(t, "transfer.pause", map[string]any{"id": ids[0], "value": false}, nil)
	e.must(t, "transfer.cancel", map[string]string{"id": ids[1]}, nil)
	for _, id := range []string{ids[0], ids[2]} {
		if j := e.waitJob(t, id); j.Status != transfer.Done {
			t.Errorf("job %s = %s", id, j.Status)
		}
	}
	if j := e.waitJob(t, ids[1]); j.Status != transfer.Cancelled {
		t.Errorf("cancelled job = %s", j.Status)
	}
	e.must(t, "transfer.remove", map[string]string{"id": ids[1]}, nil)
	if _, ok := status()[ids[1]]; ok {
		t.Error("removed job still listed")
	}
	e.must(t, "transfer.clear", nil, nil)
	if s := status(); len(s) != 0 {
		t.Errorf("clear left %d jobs", len(s))
	}
}

func testServerToServer(t *testing.T, e *e2e) {
	files := map[string]string{"a.txt": "alpha\n", "dir/b.txt": "beta\n"}
	chain := []string{"sftp", "s3", "webdav", "ftp", "scp", "sftp"}
	var prev target
	for i, proto := range chain {
		tg := e.remoteTarget(t, proto)
		if i == 0 {
			for name, c := range files {
				e.upload(t, tg.id, tg.root+"/src/"+name, []byte(c), true)
			}
		} else {
			j := e.enqueue(t, transfer.Request{Src: prev.id, Dst: tg.id, Paths: []string{prev.root + "/src"}, DstDir: tg.root, Options: e.defaults(t, prev.id, tg.id)})
			if j.Status != transfer.Done {
				t.Fatalf("%s → %s: %+v", prev.proto, proto, j)
			}
			for name, c := range files {
				if got := e.readText(t, tg.id, tg.root+"/src/"+name); got != c {
					t.Errorf("%s → %s: %s = %q", prev.proto, proto, name, got)
				}
			}
		}
		prev = tg
	}
}
