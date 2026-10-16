//go:build integration

package server

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/keys"
	"skiff/internal/prompt"
)

// testSSHConfig connects through the OpenSSH configuration of the (fake)
// home: aliases, keys, host key policy, ProxyJump, ProxyCommand, forwards.
func testSSHConfig(t *testing.T, e *e2e) {
	ssh := filepath.Join(e.home, ".ssh")
	// a key installed on the server, used only through IdentityFile
	var g keys.Generated
	e.must(t, "keys.generate", keys.GenerateRequest{Type: "ed25519", Comment: "sshconfig@e2e", Format: "openssh", SaveTo: filepath.Join(ssh, "id_sshcfg")}, &g)
	ci := e.connect(t, "sftp")
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": g.Public}, nil)

	fwd := freePort(t)
	// the first value obtained wins: cfg-strict comes before the cfg-*
	// defaults, the other blocks only add settings
	userCfg := fmt.Sprintf(`# written by the e2e test
Host cfg-strict
    StrictHostKeyChecking yes
    HostKeyAlias never-seen-before

Host cfg-*
    HostName 127.0.0.1
    Port 2222
    User skiff
    IdentityFile ~/.ssh/id_sshcfg
    IdentitiesOnly yes
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    HostKeyAlias skiff-e2e-cfg
    StrictHostKeyChecking accept-new

Host cfg-direct
    ServerAliveInterval 7
    ForwardAgent yes

Host cfg-jump
    ProxyJump cfg-direct
    LocalForward 127.0.0.1:%d 127.0.0.1:2222

Host cfg-proxycmd
    ProxyCommand nc %%h %%p
`, fwd)
	os.WriteFile(filepath.Join(ssh, "config"), []byte(userCfg), 0o600)
	os.WriteFile(e.systemSSHConfig, []byte("Host cfg-*\n    ConnectTimeout 9\n"), 0o644)

	connectAlias := func(t *testing.T, alias string) core.ConnInfo {
		t.Helper()
		site := config.Site{Protocol: "sftp", Host: alias, Name: alias, UseAgent: true, KbdInteractive: true, ResolveSymlinks: true, Keepalive: 30, Timeout: 15}
		var c core.ConnInfo
		e.must(t, "conn.open", map[string]any{"site": site}, &c)
		t.Cleanup(func() { e.call("conn.close", map[string]string{"conn": c.ID}, nil) })
		return c
	}
	logOf := func(id string) string {
		var lines []core.LogLine
		e.must(t, "conn.log", map[string]string{"conn": id}, &lines)
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(l.Msg + "\n")
		}
		return b.String()
	}

	t.Run("info", func(t *testing.T) {
		var info struct {
			Enabled    bool `json:"enabled"`
			UserFile   string
			SystemFile string
			Files      []struct {
				Path   string `json:"path"`
				Scope  string `json:"scope"`
				Exists bool   `json:"exists"`
			} `json:"files"`
			Hosts []struct {
				Alias    string `json:"alias"`
				HostName string `json:"hostName"`
				User     string `json:"user"`
				Port     int    `json:"port"`
			} `json:"hosts"`
		}
		e.must(t, "sshconfig.info", nil, &info)
		if !info.Enabled || len(info.Files) < 2 || info.Files[0].Path != "~/.ssh/config" || !info.Files[0].Exists || info.Files[1].Scope != "system" || !info.Files[1].Exists {
			t.Errorf("files %+v", info.Files)
		}
		got := map[string]string{}
		for _, h := range info.Hosts {
			got[h.Alias] = fmt.Sprintf("%s@%s:%d", h.User, h.HostName, h.Port)
		}
		if len(got) != 4 || got["cfg-direct"] != "skiff@127.0.0.1:2222" || got["cfg-*"] != "" {
			t.Errorf("hosts %v", got)
		}
	})

	t.Run("alias", func(t *testing.T) {
		before := len(e.promptsOf(prompt.HostKey))
		c := connectAlias(t, "cfg-direct")
		if c.Host != "127.0.0.1" || c.Port != 2222 || c.User != "skiff" || c.Title != "cfg-direct" {
			t.Errorf("conn info %+v", c)
		}
		if len(e.promptsOf(prompt.HostKey)) != before {
			t.Error("StrictHostKeyChecking accept-new must not ask about the key")
		}
		log := logOf(c.ID)
		for _, want := range []string{"OpenSSH configuration of cfg-direct", "HostName 127.0.0.1", "ServerAliveInterval 7", "ConnectTimeout 9", "using publickey"} {
			if !strings.Contains(log, want) {
				t.Errorf("log lacks %q:\n%s", want, log)
			}
		}
		var known []struct {
			Hosts string `json:"hosts"`
		}
		e.must(t, "hostkeys.list", nil, &known)
		found := false
		for _, k := range known {
			found = found || k.Hosts == "[skiff-e2e-cfg]:2222"
		}
		if !found {
			t.Errorf("key not recorded under the HostKeyAlias: %+v", known)
		}
		// the same alias as a URL (quick connect, scripts)
		var u core.ConnInfo
		e.must(t, "conn.open", map[string]any{"url": "sftp://cfg-direct/"}, &u)
		e.call("conn.close", map[string]string{"conn": u.ID}, nil)
	})

	t.Run("proxyJumpAndForward", func(t *testing.T) {
		c := connectAlias(t, "cfg-jump")
		if log := logOf(c.ID); !strings.Contains(log, "through jump host") {
			t.Errorf("not through the jump host:\n%s", log)
		}
		var tunnels []core.TunnelInfo
		e.must(t, "tunnel.list", nil, &tunnels)
		var tn *core.TunnelInfo
		for i := range tunnels {
			if tunnels[i].Conn == c.ID {
				tn = &tunnels[i]
			}
		}
		if tn == nil || !strings.HasPrefix(tn.Label, "LocalForward") {
			t.Fatalf("LocalForward not started: %+v", tunnels)
		}
		// the forwarded port reaches the SSH server
		nc, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwd), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		nc.SetDeadline(time.Now().Add(5 * time.Second))
		banner, _ := bufio.NewReader(nc).ReadString('\n')
		nc.Close()
		if !strings.HasPrefix(banner, "SSH-2.0-") {
			t.Errorf("banner through the forward: %q", banner)
		}
	})

	t.Run("proxyCommand", func(t *testing.T) {
		c := connectAlias(t, "cfg-proxycmd")
		if log := logOf(c.ID); !strings.Contains(log, "ProxyCommand nc %h %p") {
			t.Errorf("ProxyCommand not applied:\n%s", log)
		}
	})

	t.Run("strict", func(t *testing.T) {
		site := config.Site{Protocol: "sftp", Host: "cfg-strict", UseAgent: true, Timeout: 15}
		err := e.mustFail(t, "conn.open", map[string]any{"site": site})
		if err == nil || !strings.Contains(err.Error(), "StrictHostKeyChecking is yes") {
			t.Errorf("unknown key accepted: %v", err)
		}
	})

	t.Run("report", func(t *testing.T) {
		var r core.SSHConfigReport
		site := config.Site{Protocol: "sftp", Host: "cfg-direct", User: "someone", Keepalive: 30, Timeout: 15, UseAgent: true}
		e.must(t, "sshconfig.resolve", map[string]any{"site": site}, &r)
		st := map[string]string{}
		src := map[string]string{}
		for _, o := range r.Options {
			st[o.Key], src[o.Key] = o.Status, o.Source
		}
		if !r.Enabled || r.Host != "127.0.0.1" || r.Port != 2222 || r.User != "someone" || st["User"] != "site" || st["HostName"] != "applied" || st["ForwardAgent"] != "ignored" {
			t.Errorf("report %+v", r)
		}
		if !strings.HasPrefix(src["HostName"], "~/.ssh/config:") || !strings.HasSuffix(src["ConnectTimeout"], "ssh_config:2") {
			t.Errorf("sources %v", src)
		}
	})

	t.Run("off", func(t *testing.T) {
		// per site
		site := config.Site{Protocol: "sftp", Host: "cfg-direct", IgnoreSSHConfig: true, Timeout: 5}
		if err := e.mustFail(t, "conn.open", map[string]any{"site": site, "password": "skiff"}); err == nil {
			t.Error("connected to the alias without the configuration")
		}
		// globally
		var p config.Prefs
		e.must(t, "prefs.get", nil, &p)
		p.SSH.IgnoreConfig = true
		e.must(t, "prefs.set", p, nil)
		defer func() {
			p.SSH.IgnoreConfig = false
			e.must(t, "prefs.set", p, nil)
		}()
		site.IgnoreSSHConfig = false
		if err := e.mustFail(t, "conn.open", map[string]any{"site": site, "password": "skiff"}); err == nil {
			t.Error("connected to the alias with the configuration turned off")
		}
		var info struct {
			Enabled bool `json:"enabled"`
			Hosts   []any
			Files   []any
		}
		e.must(t, "sshconfig.info", nil, &info)
		if info.Enabled || len(info.Hosts) != 0 || len(info.Files) == 0 {
			t.Errorf("info when off %+v", info)
		}
	})
}
