//go:build integration

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/devsync"
	"skiff/internal/keys"
)

// testDevSync shares the settings of two devices through one encrypted file
// on the SFTP test server: this server is the first device, the second is
// a store and a syncer created here.
func testDevSync(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	path := root + "/skiff-sync.bin"
	const pass = "phrase partagée 2026"

	storage := testSite("sftp")
	storage.Name = "Stockage"
	st := e.saveSite(t, storage, "skiff")
	prod := testSite("sftp")
	prod.Name, prod.Host, prod.Folder = "Prod", "prod.example.com", "Clients"
	e.saveSite(t, prod, "secret-de-prod")
	var g keys.Generated
	e.must(t, "keys.generate", keys.GenerateRequest{Type: "ed25519", Comment: "partagée@e2e", Format: "openssh", Keychain: true, Name: "Clé partagée"}, &g)
	var p config.Prefs
	e.must(t, "prefs.get", nil, &p)
	p.Bookmarks = map[string][]config.Bookmark{"*": {{Name: "Journaux", Path: "/var/log", Side: "remote"}}}
	e.must(t, "prefs.set", p, nil)

	cfg := devsync.Config{SiteID: st.ID, Path: path, Device: "portable", Sites: true, Keychain: true, Prefs: true, HostKeys: true}
	if err := e.mustFail(t, "devsync.set", map[string]any{"config": cfg, "passphrase": "court"}); err == nil {
		t.Error("a short passphrase was accepted")
	}
	pp := pass
	var v devsync.View
	e.must(t, "devsync.set", map[string]any{"config": cfg, "passphrase": pp}, &v)
	if !v.HasPassphrase || v.Passphrase != "" {
		t.Errorf("view %+v", v)
	}
	var res devsync.Result
	e.must(t, "devsync.now", nil, &res)
	if !res.Created || !res.Uploaded {
		t.Fatalf("first sync %+v", res)
	}
	raw := e.download(t, ci.ID, path)
	if !strings.HasPrefix(string(raw), "SKIFFSYNC1\n") || strings.Contains(string(raw), "secret-de-prod") || strings.Contains(string(raw), "PRIVATE KEY") {
		t.Fatal("the shared file is not encrypted")
	}

	// the second device: it accepted the server key once and created the
	// storage site by hand
	dirB := filepath.Join(filepath.Dir(e.home), "device-b")
	storeB, err := config.Open(dirB, false)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(e.cfg, "known_hosts")); err == nil {
		os.WriteFile(storeB.KnownHostsPath(), b, 0o600)
	}
	sb := testSite("sftp")
	sb.Name = "Mon stockage"
	pw := "skiff"
	stB, _ := storeB.SaveSite(sb, config.SecretInput{Password: &pw})
	syncB := devsync.New(storeB, nil, storeB.KnownHostsPath())
	wrong := "pas la bonne phrase"
	cfgB := cfg
	cfgB.SiteID, cfgB.Device = stB.ID, "bureau"
	if err := syncB.SetConfig(cfgB, &wrong); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := syncB.Sync(ctx); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Errorf("wrong passphrase: %v", err)
	}
	right := pass
	syncB.SetConfig(cfgB, &right)
	rb, err := syncB.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var listA struct {
		Sites []config.SiteView `json:"sites"`
	}
	e.must(t, "sites.list", nil, &listA)
	if rb.RemoteDevice != "portable" || rb.SitesIn != len(listA.Sites) || rb.KeysIn != 1 || !rb.PrefsIn {
		t.Errorf("second device %+v", rb)
	}
	sitesB, _, _ := storeB.SitesInClear()
	var gotProd *config.Site
	storages := 0
	for i := range sitesB {
		if sitesB[i].Name == "Prod" {
			gotProd = &sitesB[i]
		}
		if sitesB[i].Name == "Stockage" || sitesB[i].Name == "Mon stockage" {
			storages++
		}
	}
	if gotProd == nil || gotProd.Password != "secret-de-prod" || gotProd.Folder != "Clients" {
		t.Errorf("Prod on the second device: %+v", gotProd)
	}
	if storages != 1 || syncB.View().SiteID != st.ID {
		for _, x := range sitesB {
			t.Logf("B site %s %q %s", x.ID, x.Name, x.Host)
		}
		t.Errorf("the storage site is there %d times (sync site %s, shared %s, B's own %s)", storages, syncB.View().SiteID, st.ID, stB.ID)
	}
	if k, _ := storeB.KeysInClear(); len(k) != 1 || k[0].Fingerprint != g.Fingerprint || !strings.Contains(k[0].Private, "PRIVATE KEY") {
		t.Errorf("keychain on the second device %+v", k)
	}
	if b := storeB.Prefs().Bookmarks["*"]; len(b) != 1 || b[0].Path != "/var/log" {
		t.Errorf("bookmarks on the second device %+v", storeB.Prefs().Bookmarks)
	}
	// nothing changed: nothing written
	if r, _ := syncB.Sync(ctx); r.Uploaded {
		t.Errorf("an unchanged sync wrote the file: %+v", r)
	}

	// the second device deletes Prod and adds Recette; the first follows
	time.Sleep(10 * time.Millisecond)
	storeB.DeleteSites([]string{gotProd.ID})
	rec := testSite("sftp")
	rec.Name, rec.Host = "Recette", "recette.example.com"
	storeB.SaveSite(rec, config.SecretInput{})
	if r, err := syncB.Sync(ctx); err != nil || !r.Uploaded {
		t.Fatalf("second device changes: %+v %v", r, err)
	}
	e.must(t, "devsync.now", nil, &res)
	if res.SitesIn != 1 || res.SitesOut != 1 || res.RemoteDevice != "bureau" {
		t.Errorf("first device %+v", res)
	}
	var list struct {
		Sites []config.SiteView `json:"sites"`
	}
	e.must(t, "sites.list", nil, &list)
	names := map[string]bool{}
	for _, s := range list.Sites {
		names[s.Name] = true
	}
	if names["Prod"] || !names["Recette"] {
		t.Errorf("sites of the first device %v", names)
	}
	e.must(t, "devsync.get", nil, &v)
	if v.State.LastStatus != "ok" || v.State.LastDevice != "bureau" || !strings.HasPrefix(v.State.Summary, "Received") {
		t.Errorf("state %+v", v.State)
	}
	// stop sharing
	cfg.Enabled = false
	e.must(t, "devsync.set", map[string]any{"config": cfg}, &v)
	for _, s := range list.Sites {
		if s.Name == "Recette" || s.Name == "Stockage" {
			e.call("sites.delete", map[string]any{"ids": []string{s.ID}}, nil)
		}
	}
}
