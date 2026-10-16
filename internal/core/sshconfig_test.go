package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
)

// sshConfigCore returns a Core whose home holds the given ~/.ssh/config.
func sshConfigCore(t *testing.T, userCfg string) (*Core, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SKIFF_SSH_SYSTEM_CONFIG", "none")
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(userCfg), 0o600)
	st, err := config.Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	c := New(st, Options{})
	t.Cleanup(c.Shutdown)
	return c, home
}

const appConfig = `
Host app
    HostName 10.1.2.3
    Port 2200
    User deploy
    IdentityFile ~/.ssh/app_key
    IdentityFile ~/.ssh/missing_key
    IdentitiesOnly yes
    ProxyJump bastion
    ServerAliveInterval 11
    ServerAliveCountMax 5
    ConnectTimeout 4
    AddressFamily inet
    Ciphers aes256-gcm@openssh.com,umac-is-not-supported
    StrictHostKeyChecking accept-new
    HostKeyAlias app-key
    UserKnownHostsFile ~/.ssh/kh_app
    LocalForward 5432 db:5432
    PasswordAuthentication no
    PreferredAuthentications publickey,keyboard-interactive
    ForwardAgent yes
    Compression yes

Host bastion
    HostName bastion.example.com
    User jump
    Port 2022
    ProxyCommand nc -X 5 -x proxy:1080 %h %p
`

func statuses(r SSHConfigReport) map[string]string {
	out := map[string]string{}
	for _, o := range r.Options {
		out[o.Key] = o.Status
	}
	return out
}

func TestSSHConfigFillsEmptySettings(t *testing.T) {
	c, home := sshConfigCore(t, appConfig)
	os.WriteFile(filepath.Join(home, ".ssh", "app_key"), []byte("not really a key"), 0o600)
	site := config.NewSite()
	site.Host = "app"
	s, err := c.resolveSSH(site, 0)
	if err != nil {
		t.Fatal(err)
	}
	tg := s.target
	if tg.Host != "10.1.2.3" || tg.Port != 2200 || tg.User != "deploy" {
		t.Errorf("target %s@%s:%d", tg.User, tg.Host, tg.Port)
	}
	if !reflect.DeepEqual(tg.KeyFiles, []string{filepath.Join(home, ".ssh", "app_key")}) || !tg.IdentitiesOnly {
		t.Errorf("keys %v identitiesOnly=%v", tg.KeyFiles, tg.IdentitiesOnly)
	}
	if !tg.NoPassword || !reflect.DeepEqual(tg.AuthOrder, []string{"publickey", "keyboard-interactive"}) {
		t.Errorf("auth: noPassword=%v order=%v", tg.NoPassword, tg.AuthOrder)
	}
	if tg.HostKeyAlias != "app-key" || tg.HostKeyPolicy.Strict != "accept-new" || !tg.HostKeyPolicy.FilesSet ||
		!reflect.DeepEqual(tg.HostKeyPolicy.Files, []string{filepath.Join(home, ".ssh", "kh_app")}) {
		t.Errorf("host key: %+v alias %s", tg.HostKeyPolicy, tg.HostKeyAlias)
	}
	if len(s.jumps) != 1 || s.jumps[0].Host != "bastion.example.com" || s.jumps[0].Port != 2022 || s.jumps[0].User != "jump" {
		t.Errorf("jumps %+v", s.jumps)
	}
	// the jump host is reached through its own ProxyCommand
	if s.proxy == nil || s.proxy.Command != "nc -X 5 -x proxy:1080 bastion.example.com 2022" || !s.proxy.Expanded {
		t.Errorf("proxy %+v", s.proxy)
	}
	if s.timeout != 4*time.Second || s.keepalive != 11*time.Second || s.keepaliveCount != 5 || s.ipVersion != "4" {
		t.Errorf("connection: %v %v %d %s", s.timeout, s.keepalive, s.keepaliveCount, s.ipVersion)
	}
	if !tg.ForwardAgent {
		t.Error("ForwardAgent yes not applied")
	}
	if !reflect.DeepEqual(s.ciphers, []string{"aes256-gcm@openssh.com"}) {
		t.Errorf("ciphers %v", s.ciphers)
	}
	if len(s.forwards) != 1 || s.forwards[0].BindPort != 5432 || s.forwards[0].DestHost != "db" || !s.forwards[0].AutoStart {
		t.Errorf("forwards %+v", s.forwards)
	}
	st := statuses(s.report)
	for k, want := range map[string]string{"HostName": "applied", "Port": "applied", "IdentityFile": "applied", "ProxyJump": "applied", "ForwardAgent": "applied", "Compression": "ignored", "LocalForward": "applied"} {
		if st[k] != want {
			t.Errorf("%s: %s, want %s", k, st[k], want)
		}
	}
	if len(s.report.Warnings) != 1 || !strings.Contains(s.report.Warnings[0], "~/.ssh/missing_key") {
		t.Errorf("warnings %v", s.report.Warnings)
	}
	if r := s.report; r.Host != "10.1.2.3" || r.User != "deploy" || len(r.Jumps) != 1 || r.Jumps[0] != "jump@bastion.example.com:2022" {
		t.Errorf("report %+v", r)
	}
}

func TestSiteSettingsWinOverSSHConfig(t *testing.T) {
	c, _ := sshConfigCore(t, appConfig)
	site := config.NewSite()
	site.Host, site.Port, site.User, site.KeyFile = "app", 2201, "root", "/explicit/key"
	site.Keepalive, site.Timeout, site.IPVersion = 60, 30, "6"
	site.Forwards = []config.Forward{{ID: "f", Type: "dynamic", BindPort: 1080}}
	site.Jumps = []config.Jump{{Host: "bastion", Port: 22}} // 22 = the editor's default
	s, err := c.resolveSSH(site, 0)
	if err != nil {
		t.Fatal(err)
	}
	tg := s.target
	if tg.Host != "10.1.2.3" || tg.Port != 2201 || tg.User != "root" || !reflect.DeepEqual(tg.KeyFiles, []string{"/explicit/key"}) {
		t.Errorf("target %s@%s:%d %v", tg.User, tg.Host, tg.Port, tg.KeyFiles)
	}
	if s.keepalive != 60*time.Second || s.timeout != 30*time.Second || s.ipVersion != "6" || len(s.forwards) != 1 || s.forwards[0].ID != "f" {
		t.Errorf("connection settings overridden: %v %v %s %+v", s.keepalive, s.timeout, s.ipVersion, s.forwards)
	}
	// the site's own jump, completed by the configuration of "bastion"
	if len(s.jumps) != 1 || s.jumps[0].Host != "bastion.example.com" || s.jumps[0].Port != 2022 || s.jumps[0].User != "jump" {
		t.Errorf("jumps %+v", s.jumps)
	}
	if s.proxy != nil {
		t.Errorf("the site has its own route, ProxyCommand of the jump must not apply: %+v", s.proxy)
	}
	st := statuses(s.report)
	for _, k := range []string{"Port", "User", "IdentityFile", "ServerAliveInterval", "ConnectTimeout", "AddressFamily", "LocalForward", "ProxyJump"} {
		if st[k] != "site" {
			t.Errorf("%s: %s, want site", k, st[k])
		}
	}
}

func TestSSHConfigCanBeTurnedOff(t *testing.T) {
	c, _ := sshConfigCore(t, appConfig)
	site := config.NewSite()
	site.Host = "app"
	site.IgnoreSSHConfig = true
	s, err := c.resolveSSH(site, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.target.Host != "app" || s.target.Port != 22 || s.report.Enabled || len(s.jumps) != 0 {
		t.Errorf("per site: %+v", s.target)
	}
	p := c.Store.Prefs()
	p.SSH.IgnoreConfig = true
	c.Store.SetPrefs(p)
	site.IgnoreSSHConfig = false
	if s, _ = c.resolveSSH(site, 0); s.target.Host != "app" || s.report.Enabled {
		t.Errorf("globally: %+v", s.target)
	}
	if c.SSHConfigReport(site).Enabled {
		t.Error("report enabled")
	}
}

func TestSSHConfigCustomFile(t *testing.T) {
	c, home := sshConfigCore(t, "")
	os.WriteFile(filepath.Join(home, "other_config"), []byte("Host x\n    HostName x.example.com\n"), 0o600)
	p := c.Store.Prefs()
	p.SSH.ConfigFile = "~/other_config"
	c.Store.SetPrefs(p)
	site := config.NewSite()
	site.Host = "x"
	if s, _ := c.resolveSSH(site, 0); s.target.Host != "x.example.com" {
		t.Errorf("custom file not read: %s", s.target.Host)
	}
}

func TestProxyJumpChain(t *testing.T) {
	c, _ := sshConfigCore(t, `
Host target
    ProxyJump hop2,admin@hop3:2222
Host hop2
    HostName hop2.example.com
    ProxyJump hop1
Host hop1
    HostName hop1.example.com
    User first
Host loop
    ProxyJump loop
`)
	site := config.NewSite()
	site.Host = "target"
	s, err := c.resolveSSH(site, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, j := range s.jumps {
		got = append(got, j.User+"@"+j.Addr())
	}
	local := s.target.User
	want := []string{"first@hop1.example.com:22", local + "@hop2.example.com:22", "admin@hop3:2222"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("chain %v, want %v", got, want)
	}
	site.Host = "loop"
	if _, err := c.resolveSSH(site, 0); err == nil {
		t.Error("ProxyJump loop accepted")
	}
}
