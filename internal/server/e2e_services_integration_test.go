//go:build integration

package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"skiff/internal/core"
)

// fakeSystemd puts stand-ins for systemctl and journalctl on the SSH test
// server (Alpine has no systemd) for the time of a test; with remove, it
// takes them away (the screenshots of the documentation leave them).
func fakeSystemd(t *testing.T, remove ...bool) {
	t.Helper()
	run := func(arg string) error {
		f, err := os.Open("../../testenv/fake-systemd.sh")
		if err != nil {
			return err
		}
		defer f.Close()
		cmd := exec.Command("docker", "exec", "-i", "-u", "0", "testenv-ssh-1", "sh", "-s", arg)
		cmd.Stdin = f
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("%s", out)
		}
		return err
	}
	if len(remove) > 0 {
		if err := run("remove"); err != nil {
			t.Fatalf("fake systemd: %v", err)
		}
		return
	}
	if err := run("install"); err != nil {
		t.Fatalf("fake systemd: %v", err)
	}
	t.Cleanup(func() { run("remove") })
}

// logsText collects the text of the logs followed live, by follower id.
func (e *e2e) logsText(t *testing.T) (wait func(id, want string) string, ended func(id string) (string, bool)) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.hs.URL, "http")+"/api/events", &websocket.DialOptions{HTTPClient: e.client})
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(64 << 20)
	type msg struct{ id, text, end string }
	ch := make(chan msg, 1000)
	go func() {
		defer ws.CloseNow()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var ev struct {
				Type string            `json:"type"`
				Data map[string]string `json:"data"`
			}
			if json.Unmarshal(data, &ev) != nil || !strings.HasPrefix(ev.Type, "logs.") {
				continue
			}
			m := msg{id: ev.Data["id"], text: ev.Data["text"]}
			if ev.Type == "logs.end" {
				m.end = "end:" + ev.Data["error"]
			}
			ch <- m
		}
	}()
	time.Sleep(100 * time.Millisecond) // subscribed before the first event
	texts := map[string]*strings.Builder{}
	ends := map[string]string{}
	pump := func(d time.Duration) bool {
		select {
		case m := <-ch:
			if texts[m.id] == nil {
				texts[m.id] = &strings.Builder{}
			}
			texts[m.id].WriteString(m.text)
			if m.end != "" {
				ends[m.id] = strings.TrimPrefix(m.end, "end:")
			}
			return true
		case <-time.After(d):
			return false
		}
	}
	wait = func(id, want string) string {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if b := texts[id]; b != nil && strings.Contains(b.String(), want) {
				return b.String()
			}
			pump(200 * time.Millisecond)
		}
		got := ""
		if b := texts[id]; b != nil {
			got = b.String()
		}
		t.Fatalf("waiting for %q in the log %s, got %q", want, id, got)
		return ""
	}
	ended = func(id string) (string, bool) {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if e, ok := ends[id]; ok {
				return e, true
			}
			pump(200 * time.Millisecond)
		}
		return "", false
	}
	return
}

func testServices(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	t.Run("noSystemd", func(t *testing.T) {
		fakeSystemd(t, true)
		if err := e.mustFail(t, "services.list", map[string]string{"conn": ci.ID}); !strings.Contains(err.Error(), "does not use systemd") {
			t.Errorf("no systemd: %v", err)
		}
	})
	t.Run("systemd", func(t *testing.T) {
		fakeSystemd(t)
		var list []core.ServiceInfo
		e.must(t, "services.list", map[string]string{"conn": ci.ID}, &list)
		by := map[string]core.ServiceInfo{}
		for _, s := range list {
			by[s.Name] = s
		}
		if len(list) != 8 || by["php8.2-fpm.service"].Active != "failed" || by["nginx.service"].Sub != "running" || by["redis-server.service"].Enabled != "disabled" {
			t.Fatalf("services %+v", list)
		}
		var d core.ServiceDetails
		e.must(t, "services.details", map[string]any{"conn": ci.ID, "name": "php8.2-fpm.service", "lines": 4}, &d)
		if d.Props["ActiveState"] != "failed" || d.Props["ExecMainStatus"] != "78" || strings.Count(d.Logs, "\n") != 3 || !strings.Contains(d.Logs, "php8.2-fpm[") {
			t.Errorf("details %+v", d)
		}
		state := func(name string) core.ServiceInfo {
			var l []core.ServiceInfo
			e.must(t, "services.list", map[string]string{"conn": ci.ID}, &l)
			for _, s := range l {
				if s.Name == name {
					return s
				}
			}
			return core.ServiceInfo{}
		}
		act := func(name, action string) {
			e.must(t, "services.action", map[string]string{"conn": ci.ID, "name": name, "action": action}, nil)
		}
		act("nginx.service", "stop")
		if s := state("nginx.service"); s.Active != "inactive" {
			t.Errorf("after stop %+v", s)
		}
		act("nginx.service", "start")
		act("redis-server.service", "enable")
		if s := state("nginx.service"); s.Active != "active" {
			t.Errorf("after start %+v", s)
		}
		if s := state("redis-server.service"); s.Enabled != "enabled" {
			t.Errorf("after enable %+v", s)
		}
		if err := e.mustFail(t, "services.action", map[string]string{"conn": ci.ID, "name": "protected.service", "action": "stop"}); !strings.Contains(err.Error(), "only root") {
			t.Errorf("not root: %v", err)
		}
		e.mustFail(t, "services.action", map[string]string{"conn": ci.ID, "name": "nginx.service; touch /tmp/x", "action": "stop"})
		e.mustFail(t, "services.action", map[string]string{"conn": ci.ID, "name": "nginx.service", "action": "mask"})
		// the journal of a service, live
		wait, ended := e.logsText(t)
		var id string
		e.must(t, "logs.follow", map[string]any{"conn": ci.ID, "unit": "nginx.service", "lines": 5}, &id)
		wait(id, "nginx[1005]")
		wait(id, "live line 2")
		e.must(t, "logs.stop", map[string]string{"id": id}, nil)
		if msg, ok := ended(id); !ok || msg != "" {
			t.Errorf("end of the journal: %q %v", msg, ok)
		}
	})
	t.Run("followFile", func(t *testing.T) {
		follow := func(t *testing.T, proto string) {
			c := e.connect(t, proto)
			p := e.remoteRoot(t, c) + "/app.log"
			var b strings.Builder
			for i := 1; i <= 50; i++ {
				b.WriteString("line " + strconv.Itoa(i) + "\n")
			}
			e.put(t, c.ID, p, b.String())
			wait, ended := e.logsText(t)
			var id string
			e.must(t, "logs.follow", map[string]any{"conn": c.ID, "path": p, "lines": 3}, &id)
			got := wait(id, "line 50\n")
			if strings.Contains(got, "line 47\n") || !strings.Contains(got, "line 48\n") {
				t.Errorf("%s: last 3 lines %q", proto, got)
			}
			// a line added on the server
			e.put(t, c.ID, p, b.String()+"ajoutée à la fin\n")
			wait(id, "ajoutée à la fin\n")
			e.must(t, "logs.stop", map[string]string{"id": id}, nil)
			if _, ok := ended(id); !ok {
				t.Errorf("%s: no end", proto)
			}
		}
		t.Run("sftpTail", func(t *testing.T) { follow(t, "sftp") })
		t.Run("ftpPoll", func(t *testing.T) { follow(t, "ftp") })
		e.mustFail(t, "logs.follow", map[string]any{"conn": ci.ID})
		if err := e.mustFail(t, "logs.follow", map[string]any{"conn": "local", "unit": "nginx.service"}); !strings.Contains(err.Error(), "shell") {
			t.Errorf("journal without a shell: %v", err)
		}
	})
}
