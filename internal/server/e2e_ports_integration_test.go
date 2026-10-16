//go:build integration

package server

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
)

// fakeWeb serves a page on 127.0.0.1:3000 of the SSH test server.
func fakeWeb(t *testing.T) {
	t.Helper()
	run := func(arg string) error {
		f, err := os.Open("../../testenv/fake-web.sh")
		if err != nil {
			return err
		}
		defer f.Close()
		cmd := exec.Command("docker", "exec", "-i", "-u", "skiff", "testenv-ssh-1", "sh", "-s", arg)
		cmd.Stdin = f
		return cmd.Run()
	}
	if err := run("start"); err != nil {
		t.Fatalf("fake web: %v", err)
	}
	t.Cleanup(func() { run("stop") })
}

// testPorts lists the ports of the SSH server and opens its internal web
// page through a tunnel; testDiskUsage measures folders (du, or a scan).
func testPorts(t *testing.T, e *e2e) {
	fakeWeb(t)
	ci := e.connect(t, "sftp")
	var ports []core.PortInfo
	e.must(t, "ports.list", map[string]string{"conn": ci.ID}, &ports)
	by := map[int]core.PortInfo{}
	for _, p := range ports {
		by[p.Port] = p
	}
	if p := by[2222]; p.Scope != "public" {
		t.Errorf("sshd %+v (all: %+v)", p, ports)
	}
	if p := by[3000]; p.Scope != "local" || p.Process != "nc" || p.Service != "HTTP" {
		t.Errorf("web page %+v (all: %+v)", p, ports)
	}
	e.mustFail(t, "ports.list", map[string]string{"conn": e.connect(t, "ftp").ID})
	// through a tunnel made on the spot (the interface asks for port 0)
	tun := e.tunnel(t, ci.ID, config.Forward{Type: "local", BindHost: "127.0.0.1", BindPort: 0, DestHost: "127.0.0.1", DestPort: 3000})
	if strings.HasSuffix(tun.Bind, ":0") {
		t.Fatalf("no port chosen: %+v", tun)
	}
	var body string
	for i := 0; i < 20 && !strings.Contains(body, "Tableau de bord interne"); i++ {
		if r, err := http.Get("http://" + tun.Bind + "/"); err == nil {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			body = string(b)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(body, "Tableau de bord interne") {
		t.Errorf("page through the tunnel: %q", body)
	}
	// the browser of this computer (SKIFF_BROWSER stands in for it)
	log := filepath.Join(t.TempDir(), "opened")
	browser := filepath.Join(t.TempDir(), "browser.sh")
	os.WriteFile(browser, []byte("#!/bin/sh\necho \"$1\" >> "+log+"\n"), 0o755)
	t.Setenv("SKIFF_BROWSER", browser)
	url := "http://" + tun.Bind + "/"
	e.must(t, "app.openURL", map[string]string{"url": url}, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(log)
		if strings.TrimSpace(string(b)) == url {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser not opened on %s: %q", url, b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.mustFail(t, "app.openURL", map[string]string{"url": "file:///etc/passwd"})
	// the clipboard of the system needs the native window (none in tests)
	if err := e.mustFail(t, "app.clipboardRead", nil); !strings.Contains(err.Error(), "native window") {
		t.Errorf("clipboard without a window: %v", err)
	}
	e.mustFail(t, "app.clipboardWrite", map[string]string{"text": "x"})
}

func testDiskUsage(t *testing.T, e *e2e) {
	measure := func(t *testing.T, proto, method string) {
		ci := e.connect(t, proto)
		root := e.remoteRoot(t, ci)
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": root + "/big"}, nil)
		e.upload(t, ci.ID, root+"/big/a.bin", make([]byte, 300<<10), true)
		e.upload(t, ci.ID, root+"/small.bin", make([]byte, 20<<10), true)
		var u core.DiskUsage
		e.must(t, "disk.usage", map[string]string{"conn": ci.ID, "path": root}, &u)
		if u.Method != method || len(u.Entries) != 2 || u.Entries[0].Name != "big" || !u.Entries[0].IsDir || u.Entries[1].Name != "small.bin" || u.Entries[1].IsDir {
			t.Fatalf("%s usage %+v", proto, u)
		}
		if u.Entries[0].Size < 300<<10 || u.Total < u.Entries[0].Size+u.Entries[1].Size {
			t.Errorf("%s sizes %+v", proto, u)
		}
		if method == "du" && (u.DiskSize == 0 || u.DiskFree == 0 || u.Mount == "") {
			t.Errorf("df %+v", u)
		}
		e.mustFail(t, "disk.usage", map[string]string{"conn": ci.ID, "path": root + "/missing"})
	}
	t.Run("sftpDu", func(t *testing.T) { measure(t, "sftp", "du") })
	t.Run("ftpScan", func(t *testing.T) { measure(t, "ftp", "scan") })
}
