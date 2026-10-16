//go:build integration

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skiff/internal/core"
)

// testProjects opens folders as projects: the list of their files and a
// search in them, by find and grep on an SSH server, by reading the files
// on FTP and on this computer; and the copies kept before each save.
func testProjects(t *testing.T, e *e2e) {
	// the same small project everywhere
	files := map[string]string{
		"index.php":               "<?php\necho 'Bonjour';\n// TODO: le titre\n",
		"conf/nginx.conf":         "server {\n  listen 80;\n  server_name example.com;\n}\n",
		"conf/sites/blog.conf":    "server_name blog.example.com;\n",
		"node_modules/x/index.js": "server_name in a dependency\n",
		"notes/café.txt":          "Un café SERVEUR\n",
	}
	dirs := []string{"conf", "conf/sites", "node_modules", "node_modules/x", "notes"}
	check := func(t *testing.T, conn, root, method string) {
		var pf core.ProjectFiles
		e.must(t, "project.files", map[string]string{"conn": conn, "root": root}, &pf)
		got := strings.Join(pf.Files, " ")
		if !strings.Contains(got, "conf/sites/blog.conf") || !strings.Contains(got, "notes/café.txt") || strings.Contains(got, "node_modules") {
			t.Errorf("files %v", pf.Files)
		}
		var res core.SearchResult
		e.must(t, "project.search", map[string]any{"conn": conn, "root": root, "opts": core.SearchOptions{Query: "server_name"}}, &res)
		if res.Method != method || res.Files != 2 || len(res.Matches) != 2 {
			t.Fatalf("server_name: %+v", res)
		}
		if m := res.Matches[0]; m.Path != "conf/nginx.conf" || m.Line != 3 || m.Col != 3 || m.Len != 11 {
			t.Errorf("first match %+v", m)
		}
		e.must(t, "project.search", map[string]any{"conn": conn, "root": root, "opts": core.SearchOptions{Query: "serveur"}}, &res)
		if len(res.Matches) != 1 || res.Matches[0].Path != "notes/café.txt" || res.Matches[0].Col != 9 {
			t.Errorf("accents: %+v", res.Matches)
		}
		e.must(t, "project.search", map[string]any{"conn": conn, "root": root, "opts": core.SearchOptions{Query: "TODO", CaseSensitive: true}}, &res)
		if len(res.Matches) != 1 || res.Matches[0].Line != 3 {
			t.Errorf("case: %+v", res.Matches)
		}
		e.must(t, "project.search", map[string]any{"conn": conn, "root": root, "opts": core.SearchOptions{Query: `listen [0-9]+`, Regex: true}}, &res)
		if len(res.Matches) != 1 || res.Matches[0].Len != 9 {
			t.Errorf("regex: %+v", res.Matches)
		}
		e.mustFail(t, "project.search", map[string]any{"conn": conn, "root": root, "opts": core.SearchOptions{}})
	}
	remote := func(t *testing.T, proto, method string) {
		ci := e.connect(t, proto)
		root := e.remoteRoot(t, ci)
		for _, d := range dirs {
			e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": root + "/" + d}, nil)
		}
		for p, c := range files {
			e.put(t, ci.ID, root+"/"+p, c)
		}
		check(t, ci.ID, root, method)
	}
	t.Run("sftpGrep", func(t *testing.T) { remote(t, "sftp", "grep") })
	t.Run("ftpScan", func(t *testing.T) { remote(t, "ftp", "scan") })
	t.Run("local", func(t *testing.T) {
		root := filepath.Join(e.home, uniq("project"))
		for p, c := range files {
			full := filepath.Join(root, filepath.FromSlash(p))
			os.MkdirAll(filepath.Dir(full), 0o755)
			os.WriteFile(full, []byte(c), 0o644)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
		check(t, "local", root, "scan")
	})
	t.Run("history", func(t *testing.T) {
		ci := e.connect(t, "sftp")
		p := e.remoteRoot(t, ci) + "/site.conf"
		e.put(t, ci.ID, p, "version 1\n")
		mode := uint32(0o750)
		e.must(t, "fs.setAttrs", map[string]any{"conn": ci.ID, "paths": []string{p}, "attrs": core.Attrs{Mode: &mode}}, nil)
		for _, c := range []string{"version 2\n", "version 3\n"} {
			e.must(t, "fs.writeText", core.WriteText{Conn: ci.ID, Path: p, Content: c, Encoding: "utf-8", EOL: "LF", Backup: true}, nil)
		}
		var vs []core.HistoryVersion
		e.must(t, "edit.history", map[string]string{"conn": ci.ID, "path": p}, &vs)
		if len(vs) != 2 {
			t.Fatalf("versions %+v", vs)
		}
		var fc core.FileContent
		e.must(t, "edit.historyRead", map[string]string{"conn": ci.ID, "path": p, "id": vs[1].ID}, &fc)
		if fc.Content != "version 1\n" {
			t.Errorf("first version %q", fc.Content)
		}
		e.mustFail(t, "edit.historyRead", map[string]string{"conn": ci.ID, "path": p, "id": "../x"})
		// the mode of the file is kept by the saves
		var st struct {
			Mode uint32 `json:"mode"`
		}
		e.must(t, "fs.stat", map[string]string{"conn": ci.ID, "path": p}, &st)
		if st.Mode&0o777 != 0o750 {
			t.Errorf("mode after the saves %o", st.Mode&0o777)
		}
	})
}
