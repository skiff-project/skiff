package config

import "testing"

func TestSecretsAndMaster(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	pw := "p@ss"
	s, err := st.SaveSite(Site{Name: "x", Host: "h", Protocol: ProtoSFTP}, SecretInput{Password: &pw})
	if err != nil {
		t.Fatal(err)
	}
	if s.Password == "p@ss" || s.Password == "" {
		t.Fatalf("password stored in clear: %q", s.Password)
	}
	r, err := st.Resolve(s)
	if err != nil || r.Password != "p@ss" {
		t.Fatalf("resolve: %v %q", err, r.Password)
	}
	// enable a master password
	if err := st.SetMasterPassword("", "master-123"); err != nil {
		t.Fatal(err)
	}
	// reopen: locked until unlocked
	st2, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.Secrets.Locked() {
		t.Fatal("expected locked store")
	}
	s2, _ := st2.Site(s.ID)
	if _, err := st2.Resolve(s2); err == nil {
		t.Fatal("expected ErrLocked")
	}
	if err := st2.Secrets.Unlock("wrong"); err != ErrWrongPassword {
		t.Fatalf("wrong password accepted: %v", err)
	}
	if err := st2.Secrets.Unlock("master-123"); err != nil {
		t.Fatal(err)
	}
	r2, err := st2.Resolve(s2)
	if err != nil || r2.Password != "p@ss" {
		t.Fatalf("resolve after unlock: %v %q", err, r2.Password)
	}
	// remove the master password
	if err := st2.SetMasterPassword("master-123", ""); err != nil {
		t.Fatal(err)
	}
	st3, _ := Open(dir, false)
	if st3.Secrets.Locked() || st3.Secrets.HasMaster() {
		t.Fatal("master password still set")
	}
	s3, _ := st3.Site(s.ID)
	if r3, err := st3.Resolve(s3); err != nil || r3.Password != "p@ss" {
		t.Fatalf("resolve after removal: %v %q", err, r3.Password)
	}
}

func TestParseURL(t *testing.T) {
	s, pw, err := ParseURL("ftpes://bob:se%40cret@files.example.com:2121/pub/")
	if err != nil {
		t.Fatal(err)
	}
	if s.Protocol != ProtoFTP || s.FTPS != "explicit" || s.Host != "files.example.com" || s.Port != 2121 || s.User != "bob" || pw != "se@cret" || s.RemoteDir != "/pub/" {
		t.Errorf("unexpected %+v %q", s, pw)
	}
	s, _, _ = ParseURL("sftp://root;fingerprint=ssh-ed25519-255-abc@host/")
	if s.HostKey != "ssh-ed25519-255-abc" || s.User != "root" {
		t.Errorf("fingerprint not parsed: %+v", s)
	}
}
