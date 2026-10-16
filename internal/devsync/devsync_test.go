package devsync

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"skiff/internal/config"
)

func TestSealOpen(t *testing.T) {
	plain := []byte(`{"sites":[{"password":"secret"}]}`)
	sealed, err := seal("phrase partagée", plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret")) || !bytes.HasPrefix(sealed, magic) {
		t.Fatal("not encrypted")
	}
	got, err := open("phrase partagée", sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %v %s", err, got)
	}
	if _, err := open("autre phrase", sealed); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := open("phrase partagée", sealed); !errors.Is(err, ErrPassphrase) {
		t.Error("a modified file was accepted")
	}
	if _, err := open("x", []byte("not a sync file")); !errors.Is(err, ErrPassphrase) {
		t.Error("foreign file")
	}
}

func site(id, name string, updated time.Time) config.Site {
	return config.Site{ID: id, Name: name, Protocol: "sftp", Host: name + ".example.com", Updated: updated}
}

func TestMergeSites(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	local := []config.Site{site("a", "a-local", t0.Add(time.Hour)), site("b", "b", t0), site("c", "c", t0), site("d", "d", t0)}
	local[1].LastUsed, local[1].UseCount = t0, 7
	remote := []config.Site{site("a", "a-remote", t0), site("b", "b-remote", t0.Add(2*time.Hour)), site("e", "e", t0), site("f", "f", t0)}
	tombs := map[string]time.Time{
		"site:c": t0.Add(time.Hour),  // deleted elsewhere after its last change
		"site:d": t0.Add(-time.Hour), // changed after its deletion: kept
		"site:f": t0.Add(time.Hour),  // deleted here, still in the file
	}
	out, in, gone := mergeSites(local, remote, tombs)
	names := map[string]string{}
	for _, s := range out {
		names[s.ID] = s.Name
	}
	want := map[string]string{"a": "a-local", "b": "b-remote", "d": "d", "e": "e"}
	if len(names) != len(want) {
		t.Fatalf("merged %v", names)
	}
	for id, n := range want {
		if names[id] != n {
			t.Errorf("%s = %q, want %q", id, names[id], n)
		}
	}
	if in != 2 || gone != 1 { // b updated and e added; c removed
		t.Errorf("in %d gone %d", in, gone)
	}
}

func TestMergeKeysAndFolders(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	local := []config.KeychainKey{{ID: "1", Name: "ancien nom", Fingerprint: "SHA256:A", Updated: t0}, {ID: "2", Fingerprint: "SHA256:B", Updated: t0}}
	remote := []config.KeychainKey{{ID: "9", Name: "nouveau nom", Fingerprint: "SHA256:A", Updated: t0.Add(time.Minute)}, {ID: "8", Fingerprint: "SHA256:C", Updated: t0}}
	out, in, gone := mergeKeys(local, remote, map[string]time.Time{"key:SHA256:B": t0.Add(time.Hour)})
	if len(out) != 2 || out[0].ID != "1" || out[0].Name != "nouveau nom" || out[1].Fingerprint != "SHA256:C" || in != 2 || gone != 1 {
		t.Errorf("keys %+v in %d gone %d", out, in, gone)
	}
	f := mergeFolders([]string{"Clients", "Prod"}, []string{"Prod", "Recette", "Vieux"}, map[string]time.Time{"folder:Vieux": t0})
	if len(f) != 3 || f[0] != "Clients" || f[2] != "Recette" {
		t.Errorf("folders %v", f)
	}
}

func TestSharedPrefsLeaveDeviceSettings(t *testing.T) {
	p := config.DefaultPrefs()
	p.LastTabs = []config.TabState{{SiteID: "x"}}
	p.UI = map[string]any{"splitter": 300}
	p.SSH.ConfigFile = "/home/me/.ssh/other"
	p.Theme = "dark"
	q := sharedPrefs(p)
	if q.LastTabs != nil || q.UI != nil || q.SSH.ConfigFile != "" || q.Theme != "dark" {
		t.Errorf("shared %+v", q)
	}
	_, h1 := prefsJSON(p)
	p.LastTabs = nil
	if _, h2 := prefsJSON(p); h1 != h2 {
		t.Error("the tabs of this device changed the hash")
	}
}
