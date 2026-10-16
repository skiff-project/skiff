//go:build integration

package server

// End-to-end tests of every Skiff feature through the HTTP API used by the
// interface, against the Docker test servers:
//
//   docker compose -f testenv/docker-compose.yml up -d
//   go test -tags integration ./internal/server/ -run TestE2E -v -timeout 30m
//
// TestE2E ends by checking that every RPC method registered by the server
// was exercised at least once.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/prompt"
	"skiff/internal/transfer"
)

// e2e is a running Skiff server (desktop mode) with an HTTP client.
type e2e struct {
	t      *testing.T
	srv    *Server
	core   *core.Core
	hs     *httptest.Server
	client *http.Client
	home   string // fake home directory (local panel, ~/.ssh)
	cfg    string // configuration directory
	opened string // file receiving the arguments of the fake xdg-open

	mu      sync.Mutex
	called  map[string]int
	prompts []core.PromptEvent // every prompt shown, in order
	policy  func(core.PromptEvent) (prompt.Response, bool)
	events  map[string]int // event types received on /api/events
	changed []string       // "conn|dir" of the fs.changed events
	stop    chan struct{}
	// system-wide ssh_config read instead of /etc/ssh/ssh_config
	systemSSHConfig string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	root := t.TempDir()
	e := &e2e{t: t, home: filepath.Join(root, "home"), cfg: filepath.Join(root, "config"), called: map[string]int{}, events: map[string]int{}, stop: make(chan struct{})}
	os.MkdirAll(filepath.Join(e.home, ".ssh"), 0o700)
	// isolate the user environment: home (keys, known_hosts), trash, opener
	t.Setenv("HOME", e.home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(e.home, ".local", "share"))
	t.Setenv("SSH_AUTH_SOCK", "")
	e.systemSSHConfig = filepath.Join(root, "ssh_config")
	t.Setenv("SKIFF_SSH_SYSTEM_CONFIG", e.systemSSHConfig)
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	e.opened = filepath.Join(root, "opened.txt")
	fake := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\n", e.opened)
	os.WriteFile(filepath.Join(bin, "xdg-open"), []byte(fake), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	st, err := config.Open(e.cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	e.core = core.New(st, core.Options{})
	e.srv = New(e.core, Options{Mode: Desktop, Version: "test", Token: "e2e-token"})
	e.hs = httptest.NewServer(e.srv)
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar, Timeout: 10 * time.Minute}
	// desktop bootstrap: the launch token becomes the session cookie
	resp, err := e.client.Get(e.hs.URL + "/?token=e2e-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	e.policy = defaultPolicy
	go e.answerPrompts()
	go e.listenEvents()
	e.startFindListener()
	t.Cleanup(func() {
		close(e.stop)
		e.core.Shutdown()
		e.hs.Close()
	})
	return e
}

// defaultPolicy accepts host keys and certificates and refuses the rest.
func defaultPolicy(ev core.PromptEvent) (prompt.Response, bool) {
	switch ev.Kind {
	case prompt.HostKey:
		return prompt.Response{Choice: "accept"}, true
	case prompt.Certificate:
		return prompt.Response{Choice: "always"}, true
	case prompt.Banner:
		return prompt.Response{Choice: "ok"}, true
	case prompt.Error:
		return prompt.Response{Choice: "abort"}, true
	}
	// passwords, overwrites…: refuse, so that a test never hangs on a question
	return prompt.Response{Choice: "cancel"}, true
}

func (e *e2e) setPolicy(p func(core.PromptEvent) (prompt.Response, bool)) {
	e.mu.Lock()
	e.policy = p
	e.mu.Unlock()
}

// answerPrompts plays the user: it answers pending prompts like the UI does.
func (e *e2e) answerPrompts() {
	seen := map[string]bool{}
	for {
		select {
		case <-e.stop:
			return
		case <-time.After(40 * time.Millisecond):
		}
		var pending []core.PromptEvent
		if err := e.call("prompt.pending", nil, &pending); err != nil {
			continue
		}
		for _, p := range pending {
			e.mu.Lock()
			if !seen[p.ID] {
				seen[p.ID] = true
				e.prompts = append(e.prompts, p)
			}
			policy := e.policy
			e.mu.Unlock()
			if r, ok := policy(p); ok {
				e.call("prompt.answer", map[string]any{"id": p.ID, "response": r}, nil)
			}
		}
	}
}

// listenEvents records the event types pushed to the interface.
func (e *e2e) listenEvents() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-e.stop; cancel() }()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.hs.URL, "http")+"/api/events", &websocket.DialOptions{HTTPClient: e.client})
	if err != nil {
		e.t.Logf("events: %v", err)
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(64 << 20)
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Conn string `json:"conn"`
				Dir  string `json:"dir"`
			} `json:"data"`
		}
		json.Unmarshal(data, &ev)
		e.mu.Lock()
		e.events[ev.Type]++
		if ev.Type == "fs.changed" {
			e.changed = append(e.changed, ev.Data.Conn+"|"+ev.Data.Dir)
		}
		e.mu.Unlock()
	}
}

func (e *e2e) promptsOf(kind prompt.Kind) []core.PromptEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []core.PromptEvent
	for _, p := range e.prompts {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}

// call invokes an RPC method like the interface does.
func (e *e2e) call(method string, params any, out any) error {
	if params == nil {
		params = struct{}{}
	}
	body, _ := json.Marshal(params)
	req, _ := http.NewRequest(http.MethodPost, e.hs.URL+"/api/rpc/"+method, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Skiff", "1")
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, data)
	}
	e.mu.Lock()
	e.called[method]++
	e.mu.Unlock()
	if env.Error != nil {
		return fmt.Errorf("%s: %s (%s)", method, env.Error.Message, env.Error.Code)
	}
	if out != nil {
		// decode into a zero value: omitted (omitempty) fields must not keep
		// the values of a previous call
		v := reflect.ValueOf(out).Elem()
		v.Set(reflect.Zero(v.Type()))
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func (e *e2e) must(t *testing.T, method string, params any, out any) {
	t.Helper()
	if err := e.call(method, params, out); err != nil {
		t.Fatal(err)
	}
}

func (e *e2e) mustFail(t *testing.T, method string, params any) error {
	t.Helper()
	err := e.call(method, params, nil)
	if err == nil {
		t.Fatalf("%s: expected an error", method)
	}
	return err
}

// ---- helpers ----

func testSite(proto string) config.Site {
	s := config.NewSite()
	s.UseAgent = false
	s.Protocol, s.Host, s.User = proto, "127.0.0.1", "skiff"
	switch proto {
	case "sftp", "scp":
		s.Port = 2222
	case "ftp":
		s.Port, s.FTPS = 2121, "explicit"
	case "webdav":
		s.Port = 8080
	case "s3":
		s.Port, s.User, s.S3PathStyle, s.S3Region = 9000, "skiffadmin", true, "us-east-1"
	}
	s.Name = "test " + proto
	return s
}

func testPassword(proto string) string {
	if proto == "s3" {
		return "skiffsecret"
	}
	return "skiff"
}

func (e *e2e) saveSite(t *testing.T, s config.Site, password string) config.SiteView {
	t.Helper()
	var v config.SiteView
	sec := config.SecretInput{}
	if password != "" {
		sec.Password = &password
	}
	e.must(t, "sites.save", map[string]any{"site": s, "secrets": sec}, &v)
	return v
}

func (e *e2e) open(t *testing.T, siteID string) core.ConnInfo {
	t.Helper()
	var ci core.ConnInfo
	e.must(t, "conn.open", map[string]any{"siteId": siteID}, &ci)
	if ci.State != "connected" {
		t.Fatalf("state = %s", ci.State)
	}
	// servers limit sessions (pure-ftpd: 5 per user): close at the end
	t.Cleanup(func() { e.call("conn.close", map[string]string{"conn": ci.ID}, nil) })
	return ci
}

func (e *e2e) connect(t *testing.T, proto string) core.ConnInfo {
	t.Helper()
	return e.open(t, e.saveSite(t, testSite(proto), testPassword(proto)).ID)
}

// waitJob waits for a transfer job to finish and returns its final state.
func (e *e2e) waitJob(t *testing.T, id string) transfer.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var jobs []transfer.Snapshot
		e.must(t, "transfer.list", nil, &jobs)
		for _, j := range jobs {
			if j.ID == id {
				switch j.Status {
				case transfer.Done, transfer.Failed, transfer.Cancelled, transfer.Partial:
					return j
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return transfer.Snapshot{}
}

func (e *e2e) enqueue(t *testing.T, req transfer.Request) transfer.Snapshot {
	t.Helper()
	var id string
	e.must(t, "transfer.enqueue", req, &id)
	return e.waitJob(t, id)
}

func (e *e2e) defaults(t *testing.T, src, dst string) transfer.Options {
	t.Helper()
	var o transfer.Options
	e.must(t, "transfer.defaults", map[string]string{"src": src, "dst": dst}, &o)
	o.Overwrite = "overwrite"
	return o
}

func (e *e2e) readText(t *testing.T, conn, p string) string {
	t.Helper()
	var fc core.FileContent
	e.must(t, "fs.readText", map[string]string{"conn": conn, "path": p}, &fc)
	return fc.Content
}

// put writes a file, replacing it (fs.createFile refuses to overwrite).
func (e *e2e) put(t *testing.T, conn, p, content string) {
	t.Helper()
	if code := e.upload(t, conn, p, []byte(content), true); code >= 300 {
		t.Fatalf("put %s: HTTP %d", p, code)
	}
}

func (e *e2e) exists(conn, p string) bool {
	return e.call("fs.stat", map[string]string{"conn": conn, "path": p}, nil) == nil
}

func eventually(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func uniq(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%1e9)
}

func (e *e2e) remoteRoot(t *testing.T, ci core.ConnInfo) string {
	t.Helper()
	dir := ci.Home
	if ci.Protocol == "s3" {
		dir = "/skiff-e2e"
		e.call("fs.mkdir", map[string]any{"conn": ci.ID, "path": dir}, nil)
	}
	root := strings.TrimRight(dir, "/") + "/" + uniq("e2e")
	e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": root}, nil)
	t.Cleanup(func() { e.call("fs.delete", map[string]any{"conn": ci.ID, "paths": []string{root}}, nil) })
	return root
}

func (e *e2e) download(t *testing.T, conn, p string) []byte {
	t.Helper()
	resp, err := e.client.Get(e.hs.URL + "/api/download?conn=" + url.QueryEscape(conn) + "&path=" + url.QueryEscape(p))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("download %s: %d %s", p, resp.StatusCode, data)
	}
	return data
}

func (e *e2e) upload(t *testing.T, conn, p string, data []byte, overwrite bool) int {
	t.Helper()
	u := e.hs.URL + "/api/upload?conn=" + url.QueryEscape(conn) + "&path=" + url.QueryEscape(p) + "&mkdirs=1"
	if overwrite {
		u += "&overwrite=1"
	}
	req, _ := http.NewRequest(http.MethodPut, u, bytes.NewReader(data))
	req.Header.Set("X-Skiff", "1")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// ---- the suite ----

func TestE2E(t *testing.T) {
	e := newE2E(t)
	steps := []struct {
		name string
		fn   func(*testing.T, *e2e)
	}{
		{"app", testApp},
		{"sites", testSites},
		{"importExport", testImportExport},
		{"protocols", testProtocols},
		{"transfers", testTransfers},
		{"queue", testQueue},
		{"serverToServer", testServerToServer},
		{"segments", testSegments},
		{"sync", testSync},
		{"tasks", testTasks},
		{"watch", testWatch},
		{"find", testFind},
		{"editors", testEditors},
		{"projects", testProjects},
		{"terminals", testTerminals},
		{"monitor", testMonitor},
		{"containers", testContainers},
		{"tunnels", testTunnels},
		{"jumpHosts", testJumpHosts},
		{"proxies", testProxies},
		{"keys", testKeys},
		{"agent", testAgent},
		{"certificates", testCertificates},
		{"keychain", testKeychain},
		{"devSync", testDevSync},
		{"hostKeys", testHostKeys},
		{"legacyAlgorithms", testLegacyAlgorithms},
		{"sshConfig", testSSHConfig},
		{"tls", testTLS},
		{"implicitFTPS", testImplicitFTPS},
		{"security", testSecurity},
		{"connections", testConnections},
	}
	for _, s := range steps {
		if !t.Run(s.name, func(t *testing.T) { s.fn(t, e) }) && os.Getenv("SKIFF_E2E_FAILFAST") != "" {
			t.FailNow()
		}
	}
	t.Run("coverage", func(t *testing.T) {
		e.mu.Lock()
		defer e.mu.Unlock()
		var missing []string
		for name := range e.srv.methods {
			if e.called[name] == 0 {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("RPC methods never exercised: %s", strings.Join(missing, ", "))
		}
		t.Logf("%d RPC methods, %d calls; events received: %v", len(e.srv.methods), func() int {
			n := 0
			for _, c := range e.called {
				n += c
			}
			return n
		}(), e.events)
		for _, ev := range []string{"conn.update", "transfer.update", "log", "prompt", "watch.event", "tunnel.update", "term.update", "find.hits", "sites.changed", "security.changed", "prefs.changed", "edit.update", "task.update", "task.run"} {
			if e.events[ev] == 0 {
				t.Errorf("event %q never received", ev)
			}
		}
	})
}
