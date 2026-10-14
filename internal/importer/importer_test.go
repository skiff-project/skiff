package importer

import (
	"fmt"
	"strings"
	"testing"
)

// winscpEncrypt mirrors WinSCP's obfuscation to produce test vectors.
func winscpEncrypt(host, user, pw string) string {
	const magic, flag = 0xA3, 0xFF
	enc := func(b int) string { return fmt.Sprintf("%02X", ^(b^magic)&0xFF) }
	full := user + host + pw
	var sb strings.Builder
	sb.WriteString(enc(flag))
	sb.WriteString(enc(0))
	sb.WriteString(enc(len(full)))
	sb.WriteString(enc(3)) // shift: 3 random chars
	sb.WriteString(enc(1) + enc(2) + enc(3))
	for i := 0; i < len(full); i++ {
		sb.WriteString(enc(int(full[i])))
	}
	sb.WriteString(enc(42))
	return sb.String()
}

func TestWinSCPDecrypt(t *testing.T) {
	enc := winscpEncrypt("example.com", "bob", "s3cr3t!é")
	got, ok := winscpDecrypt("example.com", "bob", enc)
	if !ok || got != "s3cr3t!é" {
		t.Fatalf("decrypt = %q, %v", got, ok)
	}
}

func TestWinSCPIni(t *testing.T) {
	ini := "[Configuration]\nFoo=1\n\n[Sessions\\Work/Prod%20Server]\nHostName=prod.example.com\nUserName=deploy\nPortNumber=2222\nFSProtocol=2\nRemoteDirectory=/var/www\nPassword=" +
		winscpEncrypt("prod.example.com", "deploy", "hunter2") + "\n\n[Sessions\\ftp.local]\nHostName=ftp.local\nFSProtocol=5\nFtps=3\n"
	items, err := parseWinSCPIni([]byte(ini))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	a := items[0]
	if a.Site.Name != "Prod Server" || a.Site.Folder != "Work" || a.Site.Port != 2222 || a.Site.Protocol != "sftp" || a.Password != "hunter2" || a.Site.RemoteDir != "/var/www" {
		t.Errorf("unexpected first item: %+v pw=%q", a.Site, a.Password)
	}
	if items[1].Site.Protocol != "ftp" || items[1].Site.FTPS != "explicit" {
		t.Errorf("unexpected second item: %+v", items[1].Site)
	}
}

func TestSSHConfig(t *testing.T) {
	cfg := `Host *
    ServerAliveInterval 20
Host bastion
    HostName 10.0.0.1
    User admin
Host app
    HostName app.internal
    User deploy
    Port 2200
    IdentityFile ~/.ssh/app_key
    ProxyJump bastion
    LocalForward 5432 db.internal:5432
Host *.wild
    User x
`
	items, err := parseSSHConfig([]byte(cfg), "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	app := items[1].Site
	if app.Host != "app.internal" || app.Port != 2200 || app.User != "deploy" || len(app.Jumps) != 1 || app.Jumps[0].Host != "10.0.0.1" || app.Jumps[0].User != "admin" {
		t.Errorf("unexpected app: %+v", app)
	}
	if len(app.Forwards) != 1 || app.Forwards[0].BindPort != 5432 || app.Forwards[0].DestHost != "db.internal" {
		t.Errorf("unexpected forwards: %+v", app.Forwards)
	}
	if app.Keepalive != 20 {
		t.Errorf("keepalive = %d", app.Keepalive)
	}
}

func TestFileZillaRemoteDir(t *testing.T) {
	if got := fzRemoteDir("1 0 4 home 4 user 3 www"); got != "/home/user/www" {
		t.Errorf("got %q", got)
	}
}
