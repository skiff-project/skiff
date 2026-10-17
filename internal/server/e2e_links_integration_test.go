//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"skiff/internal/instance"
	"skiff/internal/urlhandler"
)

// The links of the system (sftp://…) opened by Skiff, and the session URLs
// a second start of Skiff hands to the running window.
func testLinks(t *testing.T, e *e2e) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("the registration tested here is the freedesktop one")
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(root, "xdg"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "share"))
	t.Setenv("XDG_CURRENT_DESKTOP", "")

	var st urlhandler.Status
	e.must(t, "app.urlHandlers", nil, &st)
	if !st.Supported || len(st.Schemes) != len(urlhandler.Schemes) || st.All() {
		t.Fatalf("status before: %+v", st)
	}
	e.must(t, "app.setUrlHandlers", map[string]bool{"enable": true}, &st)
	if !st.All() {
		t.Fatalf("status after: %+v", st)
	}
	list, _ := os.ReadFile(filepath.Join(root, "config", "mimeapps.list"))
	if !strings.Contains(string(list), "x-scheme-handler/sftp=skiff.desktop") {
		t.Errorf("mimeapps.list:\n%s", list)
	}
	// no package: the menu entry of the user, which lists the schemes
	entry, _ := os.ReadFile(filepath.Join(e.home, ".local", "share", "applications", "skiff.desktop"))
	if !strings.Contains(string(entry), "--link %U") || !strings.Contains(string(entry), "x-scheme-handler/davs;") {
		t.Errorf("menu entry:\n%s", entry)
	}
	e.must(t, "app.setUrlHandlers", map[string]bool{"enable": false}, &st)
	for _, x := range st.Schemes {
		if x.Skiff {
			t.Errorf("still opened by Skiff: %+v", x)
		}
	}
	if err := e.call("app.openDefaultApps", nil, nil); err == nil {
		t.Error("no default applications to open on Linux")
	}

	// the URL of the command line, from a link: the UI asks first
	e.srv.SetStartupURL("sftp://skiff@127.0.0.1:2222/config", true)
	var info map[string]any
	e.must(t, "app.info", nil, &info)
	if info["startupUrl"] != "sftp://skiff@127.0.0.1:2222/config" || info["startupLink"] != true {
		t.Errorf("app.info: %v", info)
	}

	// a second start hands its URL over
	var raised atomic.Int32
	e.srv.opts.HandoffKey = "la-clé-du-fichier"
	e.srv.opts.Raise = func() { raised.Add(1) }
	defer func() { e.srv.opts.HandoffKey, e.srv.opts.Raise = "", nil }()
	post := func(body any, origin string) int {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, e.hs.URL+"/handoff", bytes.NewReader(b))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req) // without the cookie of the UI
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	good := map[string]any{"key": "la-clé-du-fichier", "url": "sftp://alex@serveur.example.com/var/www", "link": true}
	if c := post(map[string]any{"key": "wrong", "url": "sftp://x/"}, ""); c != http.StatusForbidden {
		t.Errorf("wrong key: %d", c)
	}
	if c := post(good, "https://attaquant.example.com"); c != http.StatusForbidden {
		t.Errorf("from a web page: %d", c)
	}
	if c := post(map[string]any{"key": "la-clé-du-fichier", "url": "pas une url"}, ""); c != http.StatusBadRequest {
		t.Errorf("not a URL: %d", c)
	}
	before := e.eventCount("app.open")
	if c := post(good, ""); c != http.StatusNoContent {
		t.Fatalf("handoff: %d", c)
	}
	eventually(t, "the app.open event", 5*time.Second, func() bool { return e.eventCount("app.open") > before })
	// a link opens only the schemes Skiff registers
	if c := post(map[string]any{"key": "la-clé-du-fichier", "url": "docker://web/etc", "link": true}, ""); c != http.StatusNoContent {
		t.Errorf("container link: %d", c)
	}
	// a start without URL only brings the window to the front
	if c := post(map[string]any{"key": "la-clé-du-fichier"}, ""); c != http.StatusNoContent {
		t.Errorf("plain start: %d", c)
	}
	var opens []instance.Request
	e.must(t, "app.takeOpens", nil, &opens)
	if len(opens) != 1 || opens[0].URL != "sftp://alex@serveur.example.com/var/www" || !opens[0].Link {
		t.Errorf("opens: %+v", opens)
	}
	e.must(t, "app.takeOpens", nil, &opens)
	if len(opens) != 0 {
		t.Errorf("taken twice: %+v", opens)
	}
	if raised.Load() != 3 {
		t.Errorf("window raised %d times, want 3", raised.Load())
	}
	// instance.Handoff finds the window through the file of its folder
	dir := t.TempDir()
	port := strings.TrimPrefix(e.hs.URL, "http://127.0.0.1:")
	os.WriteFile(filepath.Join(dir, instance.FileName), []byte(`{"pid":1,"port":`+port+`,"key":"la-clé-du-fichier"}`), 0o600)
	if !instance.Handoff(dir, instance.Request{URL: "ftp://depot.example.com/"}) {
		t.Error("handoff through the file refused")
	}
	os.WriteFile(filepath.Join(dir, instance.FileName), []byte(`{"pid":1,"port":`+port+`,"key":"une-autre"}`), 0o600)
	if instance.Handoff(dir, instance.Request{URL: "ftp://depot.example.com/"}) {
		t.Error("handoff with a stale key accepted")
	}
	e.must(t, "app.takeOpens", nil, &opens)
	if len(opens) != 1 || opens[0].Link {
		t.Errorf("opens through the file: %+v", opens)
	}
}
