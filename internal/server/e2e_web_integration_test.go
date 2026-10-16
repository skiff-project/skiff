//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skiff/internal/config"
	"skiff/internal/core"
)

// TestE2EWeb checks the web mode (skiff serve): authentication, CSRF and
// origin protections, the local root jail and the features disabled there.
func TestE2EWeb(t *testing.T) {
	newWeb := func(t *testing.T, allowTerm, confined bool) (*httptest.Server, string) {
		dir := t.TempDir()
		jail := filepath.Join(dir, "data")
		os.MkdirAll(filepath.Join(jail, "docs"), 0o755)
		os.WriteFile(filepath.Join(jail, "docs", "a.txt"), []byte("A"), 0o644)
		os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("hors de la prison"), 0o644)
		os.Symlink(dir, filepath.Join(jail, "evasion"))
		st, err := config.Open(filepath.Join(dir, "config"), false)
		if err != nil {
			t.Fatal(err)
		}
		root := jail
		if !confined {
			root = ""
		}
		c := core.New(st, core.Options{LocalRoot: root})
		s := New(c, Options{Mode: Web, Version: "test", User: "alex", PasswordHash: HashPassword("bon-mot-de-passe"), AllowLocalTerminal: allowTerm})
		hs := httptest.NewServer(s)
		t.Cleanup(func() { hs.Close(); c.Shutdown() })
		return hs, jail
	}
	type resp struct {
		code int
		body string
	}
	do := func(client *http.Client, method, u, body string, headers map[string]string) resp {
		req, _ := http.NewRequest(method, u, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return resp{r.StatusCode, string(b)}
	}
	login := func(client *http.Client, base, user, pw string) resp {
		b, _ := json.Marshal(map[string]string{"user": user, "password": pw})
		return do(client, "POST", base+"/api/auth/login", string(b), map[string]string{"X-Skiff": "1"})
	}
	rpc := func(client *http.Client, base, method string, params any) resp {
		b, _ := json.Marshal(params)
		return do(client, "POST", base+"/api/rpc/"+method, string(b), map[string]string{"X-Skiff": "1"})
	}

	hs, jail := newWeb(t, false, true)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	t.Run("authentication", func(t *testing.T) {
		if r := rpc(client, hs.URL, "app.info", nil); r.code != 401 {
			t.Fatalf("API without session: %d", r.code)
		}
		if r := do(client, "GET", hs.URL+"/api/auth/status", "", nil); !strings.Contains(r.body, `"authenticated":false`) || !strings.Contains(r.body, `"mode":"web"`) {
			t.Errorf("status = %s", r.body)
		}
		if r := login(client, hs.URL, "alex", "faux"); r.code != 401 {
			t.Errorf("wrong password: %d", r.code)
		}
		if r := login(client, hs.URL, "bob", "bon-mot-de-passe"); r.code != 401 {
			t.Errorf("wrong user: %d", r.code)
		}
		if r := login(client, hs.URL, "alex", "bon-mot-de-passe"); r.code != 200 {
			t.Fatalf("login: %d %s", r.code, r.body)
		}
		u := hs.URL + "/"
		req, _ := http.NewRequest("GET", u, nil)
		for _, c := range jar.Cookies(req.URL) {
			if c.Name == "skiff_session" && c.Value == "" {
				t.Error("empty session cookie")
			}
		}
		if r := rpc(client, hs.URL, "app.info", nil); r.code != 200 || !strings.Contains(r.body, `"mode":"web"`) {
			t.Fatalf("app.info after login: %d %s", r.code, r.body)
		}
	})
	t.Run("csrfAndOrigin", func(t *testing.T) {
		if r := do(client, "POST", hs.URL+"/api/rpc/app.info", "{}", nil); r.code != 403 {
			t.Errorf("without X-Skiff: %d", r.code)
		}
		if r := do(client, "POST", hs.URL+"/api/rpc/app.info", "{}", map[string]string{"X-Skiff": "1", "Origin": "https://evil.example"}); r.code != 403 {
			t.Errorf("cross-origin: %d", r.code)
		}
	})
	t.Run("rateLimit", func(t *testing.T) {
		other := &http.Client{}
		codes := []int{}
		for i := 0; i < 6; i++ {
			codes = append(codes, login(other, hs.URL, "alex", "faux").code)
		}
		if codes[len(codes)-1] != 429 {
			t.Errorf("after repeated failures: %v", codes)
		}
		if r := login(other, hs.URL, "alex", "bon-mot-de-passe"); r.code != 429 {
			t.Errorf("even the right password is refused while blocked: %d", r.code)
		}
	})
	t.Run("localJail", func(t *testing.T) {
		// paths stay real paths; the home folder is the jail
		var info struct {
			Result struct {
				LocalHome string `json:"localHome"`
				LocalRoot string `json:"localRoot"`
				Terminal  bool   `json:"localTerminal"`
			} `json:"result"`
		}
		json.Unmarshal([]byte(rpc(client, hs.URL, "app.info", nil).body), &info)
		if info.Result.LocalHome != jail || info.Result.LocalRoot != jail || info.Result.Terminal {
			t.Fatalf("app.info = %+v", info.Result)
		}
		r := rpc(client, hs.URL, "fs.list", map[string]string{"conn": "local", "path": jail})
		if r.code != 200 || !strings.Contains(r.body, `"docs"`) {
			t.Fatalf("jail root: %d %s", r.code, r.body)
		}
		if r := rpc(client, hs.URL, "fs.list", map[string]string{"conn": "local", "path": "/"}); r.code != 403 {
			t.Errorf("listing / outside the jail: %d", r.code)
		}
		for _, p := range []string{jail + "/../secret.txt", jail + "/evasion/secret.txt", "/etc/passwd"} {
			r = rpc(client, hs.URL, "fs.readText", map[string]string{"conn": "local", "path": p})
			if r.code == 200 || strings.Contains(r.body, "hors de la prison") {
				t.Errorf("escaped the jail with %s: %s", p, r.body)
			}
		}
		r = do(client, "GET", hs.URL+"/api/download?conn=local&path="+jail+"/docs/a.txt", "", nil)
		if r.code != 200 || r.body != "A" {
			t.Errorf("download in the jail: %d %q", r.code, r.body)
		}
		r = do(client, "PUT", hs.URL+"/api/upload?conn=local&path="+jail+"/docs/b.txt", "B", map[string]string{"X-Skiff": "1"})
		if r := do(client, "PUT", hs.URL+"/api/upload?conn=local&path="+jail+"/../evade.txt", "x", map[string]string{"X-Skiff": "1"}); r.code < 400 {
			t.Errorf("upload outside the jail: %d", r.code)
		}
		if b, _ := os.ReadFile(filepath.Join(jail, "docs", "b.txt")); r.code >= 300 || string(b) != "B" {
			t.Errorf("upload in the jail: %d", r.code)
		}
	})
	t.Run("disabledFeatures", func(t *testing.T) {
		for _, c := range []struct {
			method string
			params any
		}{
			{"fs.exec", map[string]string{"conn": "local", "command": "id"}},
			{"term.open", map[string]any{"conn": "local", "cols": 80, "rows": 24}},
			{"edit.external", map[string]string{"conn": "local", "path": "/docs/a.txt"}},
			{"fs.open", map[string]string{"conn": "local", "path": "/docs/a.txt"}},
			{"fs.reveal", map[string]string{"conn": "local", "path": "/docs/a.txt"}},
			{"sites.importPreview", map[string]string{"source": "winscp-ini", "path": "/etc/passwd"}},
			{"keys.convert", map[string]string{"path": "/etc/ssh/ssh_host_ed25519_key", "format": "ppk"}},
			{"keychain.add", map[string]string{"path": "/etc/ssh/ssh_host_ed25519_key"}},
			{"containers.list", map[string]string{"runtime": "docker"}},
			{"app.clipboardRead", nil},
			{"app.clipboardWrite", map[string]string{"text": "x"}},
			{"app.openURL", map[string]string{"url": "http://127.0.0.1:1/"}},
			{"conn.open", map[string]any{"site": map[string]any{"protocol": "container", "host": "x", "containerRuntime": "docker"}}},
			{"conn.open", map[string]any{"url": "docker://x/"}},
		} {
			if r := rpc(client, hs.URL, c.method, c.params); r.code == 200 {
				t.Errorf("%s must be refused in web mode: %s", c.method, r.body)
			}
		}
		// pasted content is accepted
		if r := rpc(client, hs.URL, "sites.importPreview", map[string]string{"source": "urls", "data": "sftp://a@b/"}); r.code != 200 {
			t.Errorf("pasted import: %d %s", r.code, r.body)
		}
	})
	t.Run("logout", func(t *testing.T) {
		if r := do(client, "POST", hs.URL+"/api/auth/logout", "", map[string]string{"X-Skiff": "1"}); r.code != 200 {
			t.Fatalf("logout: %d", r.code)
		}
		if r := rpc(client, hs.URL, "app.info", nil); r.code != 401 {
			t.Errorf("API after logout: %d", r.code)
		}
	})
	t.Run("allowLocalTerminal", func(t *testing.T) {
		hs2, _ := newWeb(t, true, false)
		jar2, _ := cookiejar.New(nil)
		c2 := &http.Client{Jar: jar2}
		if r := login(c2, hs2.URL, "alex", "bon-mot-de-passe"); r.code != 200 {
			t.Fatal(r.body)
		}
		r := rpc(c2, hs2.URL, "term.open", map[string]any{"conn": "local", "cols": 80, "rows": 24})
		if r.code != 200 {
			t.Errorf("--allow-local-terminal: %d %s", r.code, r.body)
		}
		if r := rpc(c2, hs2.URL, "app.info", nil); !strings.Contains(r.body, `"localTerminal":true`) {
			t.Errorf("app.info = %s", r.body)
		}
		// never with a confined local panel, even when allowed
		hs3, _ := newWeb(t, true, true)
		jar3, _ := cookiejar.New(nil)
		c3 := &http.Client{Jar: jar3}
		login(c3, hs3.URL, "alex", "bon-mot-de-passe")
		if r := rpc(c3, hs3.URL, "term.open", map[string]any{"conn": "local", "cols": 80, "rows": 24}); r.code == 200 || !strings.Contains(r.body, "--root") {
			t.Errorf("terminal with --root: %d %s", r.code, r.body)
		}
	})
}
