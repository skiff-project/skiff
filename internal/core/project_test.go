package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skiff/internal/config"
)

func projectCore(t *testing.T) (*Core, string) {
	t.Helper()
	st, err := config.Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	c := New(st, Options{})
	t.Cleanup(c.Shutdown)
	root := t.TempDir()
	files := map[string]string{
		"index.php":                 "<?php\necho 'Bonjour';\n// TODO: titre\n",
		"conf/nginx.conf":           "server {\n  listen 80;\n  server_name example.com;\n}\n",
		"conf/sites/blog.conf":      "server_name blog.example.com;\n",
		"node_modules/x/index.js":   "TODO in a dependency\n",
		".git/config":               "[core] TODO\n",
		"image.png":                 "\x89PNG\x00\x00TODO",
		"notes/café.txt":            "Un café SERVEUR\n",
		"notes/latin1.txt":          "\xe9t\xe9 server_name\n", // windows-1252
		"logs/a/b/c/deep/trace.log": "server_name deep\n",
	}
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return c, root
}

func TestProjectFiles(t *testing.T) {
	c, root := projectCore(t)
	res, err := c.ProjectFiles(context.Background(), LocalID, root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(res.Files, " ")
	for _, want := range []string{"index.php", "conf/nginx.conf", "conf/sites/blog.conf", "logs/a/b/c/deep/trace.log", "notes/café.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing from %v", want, res.Files)
		}
	}
	if strings.Contains(got, "node_modules") || strings.Contains(got, ".git/") {
		t.Errorf("excluded folders listed: %v", res.Files)
	}
	// the folders to skip come from the preferences
	p := c.Store.Prefs()
	p.Editor.ProjectExcludes = "conf, logs"
	c.Store.SetPrefs(p)
	res, _ = c.ProjectFiles(context.Background(), LocalID, root)
	got = strings.Join(res.Files, " ")
	if strings.Contains(got, "conf/") || strings.Contains(got, "logs/") || !strings.Contains(got, "node_modules/x/index.js") {
		t.Errorf("with the preference: %v", res.Files)
	}
}

func TestProjectSearch(t *testing.T) {
	c, root := projectCore(t)
	ctx := context.Background()
	res, err := c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: "server_name"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != "scan" || res.Files != 4 || len(res.Matches) != 4 {
		t.Fatalf("server_name: %+v", res)
	}
	for _, m := range res.Matches {
		if m.Path == "conf/nginx.conf" && (m.Line != 3 || m.Col != 3 || m.Len != 11 || m.Text != "  server_name example.com;") {
			t.Errorf("nginx match %+v", m)
		}
		if m.Path == "notes/latin1.txt" && !strings.HasPrefix(m.Text, "été") {
			t.Errorf("windows-1252 line %+v", m)
		}
	}
	// case, binary files, excluded folders
	res, _ = c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: "todo"})
	if len(res.Matches) != 1 || res.Matches[0].Path != "index.php" || res.Matches[0].Line != 3 {
		t.Errorf("todo: %+v", res.Matches)
	}
	res, _ = c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: "todo", CaseSensitive: true})
	if len(res.Matches) != 0 {
		t.Errorf("case-sensitive: %+v", res.Matches)
	}
	// characters after an accent: the column counts characters
	res, _ = c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: "serveur"})
	if len(res.Matches) != 1 || res.Matches[0].Col != 9 || res.Matches[0].Len != 7 {
		t.Errorf("serveur: %+v", res.Matches)
	}
	res, err = c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: `listen\s+\d+`, Regex: true})
	if err != nil || len(res.Matches) != 1 || res.Matches[0].Len != 9 {
		t.Errorf("regex: %+v %v", res.Matches, err)
	}
	if _, err := c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: "(", Regex: true}); err == nil {
		t.Error("an invalid regular expression must fail")
	}
	res, _ = c.ProjectSearch(ctx, LocalID, root, SearchOptions{Query: "e", MaxResults: 3})
	if len(res.Matches) != 3 || !res.Truncated {
		t.Errorf("limit: %d matches, truncated %v", len(res.Matches), res.Truncated)
	}
}

func TestMakeMatchLongLine(t *testing.T) {
	find, _ := SearchOptions{Query: "needle"}.matcher()
	line := strings.Repeat("a", 1000) + "needle" + strings.Repeat("b", 1000)
	m, ok := makeMatch(find, "f", 1, line)
	if !ok || len([]rune(m.Text)) > maxMatchText+1 || string([]rune(m.Text)[m.Col-1:m.Col-1+m.Len]) != "needle" {
		t.Errorf("long line: col %d len %d %q", m.Col, m.Len, m.Text)
	}
}

func TestHistory(t *testing.T) {
	c, root := projectCore(t)
	ctx := context.Background()
	p := filepath.Join(root, "conf", "nginx.conf")
	save := func(content string) {
		t.Helper()
		if _, err := c.WriteText(ctx, WriteText{Conn: LocalID, Path: p, Content: content, Encoding: "utf-8", EOL: "LF", Backup: true}); err != nil {
			t.Fatal(err)
		}
	}
	save("version 2\n")
	save("version 3\n")
	save("version 3\n") // same content as the latest copy: not kept twice
	vs, err := c.History(LocalID, p)
	if err != nil || len(vs) != 2 {
		t.Fatalf("history %+v %v", vs, err)
	}
	newest, err := c.HistoryRead(LocalID, p, vs[0].ID, "")
	if err != nil || newest.Content != "version 2\n" {
		t.Errorf("newest copy %q %v", newest.Content, err)
	}
	oldest, _ := c.HistoryRead(LocalID, p, vs[1].ID, "")
	if !strings.HasPrefix(oldest.Content, "server {") {
		t.Errorf("first copy %q", oldest.Content)
	}
	if _, err := c.HistoryRead(LocalID, p, "../../prefs", ""); err == nil {
		t.Error("a version id must be a number")
	}
	// only the last ones are kept
	for i := 0; i < historyKeep+5; i++ {
		save("v" + string(rune('a'+i)) + "\n")
	}
	if vs, _ := c.History(LocalID, p); len(vs) != historyKeep {
		t.Errorf("%d copies kept", len(vs))
	}
	// no copy when the history is off
	pr := c.Store.Prefs()
	pr.Editor.NoHistory = true
	c.Store.SetPrefs(pr)
	other := filepath.Join(root, "index.php")
	if _, err := c.WriteText(ctx, WriteText{Conn: LocalID, Path: other, Content: "x", Encoding: "utf-8", EOL: "LF", Backup: true}); err != nil {
		t.Fatal(err)
	}
	if vs, _ := c.History(LocalID, other); len(vs) != 0 {
		t.Errorf("history off: %+v", vs)
	}
}
