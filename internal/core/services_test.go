package core

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseServices(t *testing.T) {
	out := `  nginx.service              loaded    active   running Web server
● php-fpm.service            loaded    failed   failed  The PHP FastCGI Process Manager
  ssh.service                loaded    active   running OpenBSD Secure Shell server
  apt-daily.timer            loaded    active   waiting Daily apt download activities
SKIFF:FILES
nginx.service                enabled         enabled
php-fpm.service              enabled         enabled
ssh.service                  enabled         enabled
backup.service               disabled        enabled
getty@.service               enabled         enabled
`
	got := parseServices(out)
	names := []string{}
	for _, s := range got {
		names = append(names, s.Name)
	}
	if strings.Join(names, " ") != "backup.service nginx.service php-fpm.service ssh.service" {
		t.Fatalf("services %v", names)
	}
	php := got[2]
	if php.Active != "failed" || php.Sub != "failed" || php.Enabled != "enabled" || php.Description != "The PHP FastCGI Process Manager" {
		t.Errorf("php-fpm %+v", php)
	}
	if b := got[0]; b.Load != "not-loaded" || b.Active != "inactive" || b.Enabled != "disabled" {
		t.Errorf("not loaded %+v", b)
	}
	if e := systemdError("System has not been booted with systemd as init system (PID 1). Can't operate.", nil); e != ErrNoSystemd {
		t.Errorf("no systemd: %v", e)
	}
	if e := systemdError("Failed to start nginx.service: Interactive authentication required.", nil); e != ErrNeedRoot {
		t.Errorf("not root: %v", e)
	}
}

func TestArchivesLocal(t *testing.T) {
	c, root := projectCore(t)
	ctx := context.Background()
	for _, format := range []string{"zip", "tar.gz", "tar"} {
		name := "site." + format
		p, err := c.Compress(ctx, CompressRequest{Conn: LocalID, Dir: root, Names: []string{"conf", "index.php"}, Archive: name, Format: format})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if _, err := c.Compress(ctx, CompressRequest{Conn: LocalID, Dir: root, Names: []string{"index.php"}, Archive: name, Format: format}); err == nil {
			t.Errorf("%s: an existing archive must not be replaced", format)
		}
		dest := filepath.Join(root, "out-"+format)
		if err := c.Extract(ctx, ExtractRequest{Conn: LocalID, Archive: p, Dest: dest}); err != nil {
			t.Fatalf("extract %s: %v", format, err)
		}
		for rel, want := range map[string]string{"index.php": "<?php\necho 'Bonjour';\n// TODO: titre\n", "conf/sites/blog.conf": "server_name blog.example.com;\n"} {
			b, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
			if err != nil || string(b) != want {
				t.Errorf("%s %s: %q %v", format, rel, b, err)
			}
		}
	}
	if _, err := c.Compress(ctx, CompressRequest{Conn: LocalID, Dir: root, Names: []string{"../x"}, Archive: "a.zip", Format: "zip"}); err == nil {
		t.Error("a name with a slash must be refused")
	}
	// a member that would land outside the destination ("zip slip")
	evil := filepath.Join(root, "evil.zip")
	zf, _ := os.Create(evil)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("../../escaped.txt")
	w.Write([]byte("x"))
	zw.Close()
	zf.Close()
	if err := c.Extract(ctx, ExtractRequest{Conn: LocalID, Archive: evil, Dest: filepath.Join(root, "evil")}); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Errorf("zip slip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escaped.txt")); err == nil {
		t.Error("a file was written outside the destination")
	}
	if err := c.Extract(ctx, ExtractRequest{Conn: LocalID, Archive: filepath.Join(root, "index.php"), Dest: root}); err != ErrArchiveKind {
		t.Errorf("not an archive: %v", err)
	}
}

func TestFollowLocalFile(t *testing.T) {
	c, root := projectCore(t)
	p := filepath.Join(root, "app.log")
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, "line "+itoa(i))
	}
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	events, unsub := c.Bus.Subscribe()
	defer unsub()
	id, err := c.Follow(FollowRequest{Conn: LocalID, Path: p, Lines: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopFollow(id)
	var got strings.Builder
	wait := func(want string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for !strings.Contains(got.String(), want) {
			select {
			case ev := <-events:
				if ev.Type == "logs.data" {
					if m := ev.Data.(map[string]string); m["id"] == id {
						got.WriteString(m["text"])
					}
				}
			case <-deadline:
				t.Fatalf("waiting for %q, got %q", want, got.String())
			}
		}
	}
	wait("line 300\n")
	if strings.Contains(got.String(), "line 295\n") || !strings.Contains(got.String(), "line 296\n") {
		t.Errorf("last 5 lines: %q", got.String())
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("une ligne ajoutée, été\n")
	f.Close()
	wait("une ligne ajoutée, été\n")
	c.StopFollow(id)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "logs.end" && ev.Data.(map[string]string)["id"] == id {
				if e := ev.Data.(map[string]string)["error"]; e != "" {
					t.Errorf("end: %s", e)
				}
				return
			}
		case <-deadline:
			t.Fatal("no end after stop")
		}
	}
}
