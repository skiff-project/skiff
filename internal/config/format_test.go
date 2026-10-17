package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatOfANewFolder(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "format.json")); strings.TrimSpace(string(b)) != `{"format":1}` {
		t.Errorf("format.json = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Error("a new folder has nothing to back up")
	}
}

// A folder of Skiff before 1.0 (no format.json) is backed up, then marked.
func TestFormatOfAnOlderFolder(t *testing.T) {
	dir := t.TempDir()
	sites := `{"version":1,"folders":["Clients"],"sites":[{"id":"s1","name":"Ancien","protocol":"sftp","host":"h"}]}`
	os.WriteFile(filepath.Join(dir, "sites.json"), []byte(sites), 0o600)
	os.WriteFile(filepath.Join(dir, "known_hosts"), []byte("h ssh-ed25519 AAAA\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "logs"), 0o700)
	os.WriteFile(filepath.Join(dir, "logs", "big.log"), []byte("not copied"), 0o600)
	st, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Sites()) != 1 || st.Sites()[0].Name != "Ancien" {
		t.Errorf("sites after the migration: %+v", st.Sites())
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "backups", "*-format-0"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	if b, _ := os.ReadFile(filepath.Join(backups[0], "sites.json")); string(b) != sites {
		t.Errorf("sites.json not backed up: %q", b)
	}
	if _, err := os.Stat(filepath.Join(backups[0], "known_hosts")); err != nil {
		t.Error("known_hosts not backed up")
	}
	if _, err := os.Stat(filepath.Join(backups[0], "logs")); !os.IsNotExist(err) {
		t.Error("the folders are not backed up")
	}
	// opened again: up to date, no other backup
	if _, err := Open(dir, false); err != nil {
		t.Fatal(err)
	}
	if again, _ := filepath.Glob(filepath.Join(dir, "backups", "*")); len(again) != 1 {
		t.Errorf("backed up again: %v", again)
	}
}

// A folder of a newer Skiff is refused, untouched.
func TestFormatOfANewerFolder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "format.json"), []byte(`{"format":7}`), 0o600)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme":"dark","futureOption":true}`), 0o600)
	_, err := Open(dir, false)
	var newer ErrNewerFormat
	if !errors.As(err, &newer) || newer.Format != 7 || !strings.Contains(err.Error(), "newer version of Skiff") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); !strings.Contains(string(b), "futureOption") {
		t.Error("the newer configuration was changed")
	}
}

func TestBackupsKeepTheLastOnes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sites.json"), []byte(`{}`), 0o600)
	for i := 0; i < keptBackups+3; i++ {
		os.MkdirAll(filepath.Join(dir, "backups", "2020010"+string(rune('0'+i))+"-000000-format-0"), 0o700)
	}
	if err := backup(dir, 0); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(filepath.Join(dir, "backups"))
	if len(left) != keptBackups {
		t.Errorf("%d backups kept, want %d", len(left), keptBackups)
	}
}
