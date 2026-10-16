//go:build integration

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/importer"
	"skiff/internal/vfs"
)

func testApp(t *testing.T, e *e2e) {
	var info map[string]any
	e.must(t, "app.info", nil, &info)
	if info["mode"] != "desktop" || info["version"] != "test" || info["localHome"] != e.home || info["hasMaster"] != false || info["locked"] != false {
		t.Fatalf("app.info = %v", info)
	}
	var p config.Prefs
	e.must(t, "prefs.get", nil, &p)
	if !p.AutoReconnect || p.Transfer.Overwrite != "ask" {
		t.Errorf("unexpected defaults: %+v", p)
	}
	p.Theme, p.Accent, p.Language, p.Density = "dark", "#14b8a6", "fr", "compact"
	p.Queue.MaxParallel = 4
	e.must(t, "prefs.set", p, nil)
	var p2 config.Prefs
	e.must(t, "prefs.get", nil, &p2)
	if p2.Theme != "dark" || p2.Accent != "#14b8a6" || p2.Language != "fr" || p2.Density != "compact" || p2.Queue.MaxParallel != 4 {
		t.Fatalf("prefs not saved: %+v", p2)
	}
	// persisted on disk
	data, _ := os.ReadFile(filepath.Join(e.cfg, "settings.json"))
	if !strings.Contains(string(data), `"#14b8a6"`) {
		t.Errorf("settings.json not written")
	}
}

func testSites(t *testing.T, e *e2e) {
	s := testSite("sftp")
	s.Name, s.Folder, s.Notes, s.Color, s.RemoteDir = "Production web", "Clients/Acme", "notes de déploiement", "#22c55e", "/config"
	v := e.saveSite(t, s, "skiff")
	if !v.HasPassword || v.Password != "" {
		t.Fatalf("the view must flag the password without exposing it: %+v", v)
	}
	raw, _ := os.ReadFile(filepath.Join(e.cfg, "sites.json"))
	if strings.Contains(string(raw), `"password":"skiff"`) {
		t.Fatal("password stored in clear text")
	}
	var list struct {
		Folders []string          `json:"folders"`
		Sites   []config.SiteView `json:"sites"`
	}
	e.must(t, "sites.list", nil, &list)
	found := false
	for _, x := range list.Sites {
		found = found || x.ID == v.ID
	}
	if !found {
		t.Fatal("saved site not listed")
	}
	// duplicate (secrets included, localized name)
	var dup config.SiteView
	e.must(t, "sites.duplicate", map[string]string{"id": v.ID, "name": "Production web (copie)"}, &dup)
	if dup.Name != "Production web (copie)" || !dup.HasPassword || dup.ID == v.ID {
		t.Fatalf("duplicate = %+v", dup)
	}
	ci := e.open(t, dup.ID) // the copied password works
	e.must(t, "conn.close", map[string]string{"conn": ci.ID}, nil)
	// folders
	e.must(t, "folders.add", map[string]string{"name": "Vide"}, nil)
	e.must(t, "folders.rename", map[string]string{"name": "Vide", "to": "Archives"}, nil)
	e.must(t, "sites.move", map[string]any{"ids": []string{dup.ID}, "folder": "Archives"}, nil)
	e.must(t, "sites.list", nil, &list)
	for _, x := range list.Sites {
		if x.ID == dup.ID && x.Folder != "Archives" {
			t.Fatalf("move: folder = %q", x.Folder)
		}
	}
	e.must(t, "folders.delete", map[string]any{"name": "Archives", "withSites": false}, nil)
	e.must(t, "sites.list", nil, &list)
	for _, x := range list.Sites {
		if x.ID == dup.ID && x.Folder != "" {
			t.Fatalf("deleting a folder must keep its sites (folder = %q)", x.Folder)
		}
	}
	for _, f := range list.Folders {
		if f == "Archives" {
			t.Fatal("folder not deleted")
		}
	}
	// URLs
	var u string
	e.must(t, "sites.url", map[string]any{"id": v.ID}, &u)
	if u != "sftp://skiff@127.0.0.1:2222/config" {
		t.Errorf("url = %s", u)
	}
	e.must(t, "sites.url", map[string]any{"id": v.ID, "withPassword": true}, &u)
	if u != "sftp://skiff:skiff@127.0.0.1:2222/config" {
		t.Errorf("url with password = %s", u)
	}
	var parsed struct {
		Site     config.Site `json:"site"`
		Password string      `json:"password"`
	}
	for in, want := range map[string]string{
		"ftpes://depot:p%40ss@ftp.example.com:2121/exports": "ftp explicit depot p@ss /exports",
		"ftps://depot@ftp.example.com/":                     "ftp implicit depot  ",
		"davs://alex@cloud.example.com/remote.php/dav/":     "webdav  alex  /remote.php/dav/",
		"s3://AKIA:secret@s3.example.com/bucket/dir":        "s3  AKIA secret /bucket/dir",
		"alex@serveur": "sftp  alex  ",
	} {
		e.must(t, "sites.parseUrl", map[string]string{"url": in}, &parsed)
		ftps := ""
		if parsed.Site.Protocol == "ftp" {
			ftps = parsed.Site.FTPS
		}
		got := strings.Join([]string{parsed.Site.Protocol, ftps, parsed.Site.User, parsed.Password, parsed.Site.RemoteDir}, " ")
		if got != want {
			t.Errorf("parseUrl(%s) = %q, want %q", in, got, want)
		}
	}
	e.must(t, "sites.parseUrl", map[string]string{"url": "sftp://alex;fingerprint=SHA256:abc@serveur/"}, &parsed)
	if parsed.Site.HostKey != "SHA256:abc" || parsed.Site.User != "alex" {
		t.Errorf("WinSCP fingerprint syntax: %+v", parsed.Site)
	}
	e.mustFail(t, "sites.parseUrl", map[string]string{"url": "gopher://x"})
	e.must(t, "sites.delete", map[string]any{"ids": []string{dup.ID}}, nil)
	e.must(t, "sites.list", nil, &list)
	for _, x := range list.Sites {
		if x.ID == dup.ID {
			t.Fatal("site not deleted")
		}
	}
}

func testImportExport(t *testing.T, e *e2e) {
	s := testSite("ftp")
	s.Name, s.Folder = "Export test", "Partenaires"
	v := e.saveSite(t, s, "Tr3s-S3cret!")
	for _, format := range []string{"json", "winscp-ini", "ssh-config", "urls"} {
		var out string
		e.must(t, "sites.export", map[string]any{"ids": []string{v.ID}, "format": format}, &out)
		if strings.Contains(out, "Tr3s-S3cret") {
			t.Errorf("%s export contains a password", format)
		}
		if format != "ssh-config" && !strings.Contains(out, "127.0.0.1") {
			t.Errorf("%s export: %s", format, out)
		}
	}
	var sources []importer.Source
	e.must(t, "sites.importSources", nil, &sources)
	ids := map[string]bool{}
	for _, s := range sources {
		ids[s.ID] = true
	}
	for _, id := range []string{"winscp-ini", "putty", "filezilla", "ssh-config", "skiff-json", "urls"} {
		if !ids[id] {
			t.Errorf("import source %s missing", id)
		}
	}
	// a WinSCP.ini as written by WinSCP
	ini := `[Sessions\Clients/Serveur%20web]
HostName=127.0.0.1
PortNumber=2222
UserName=skiff
FSProtocol=2
RemoteDirectory=%2Fconfig
LocalDirectory=C:%5CSites
`
	var exported string
	e.must(t, "sites.export", map[string]any{"ids": []string{v.ID}, "format": "winscp-ini"}, &exported)
	var putty = filepath.Join(e.home, ".putty", "sessions")
	os.MkdirAll(putty, 0o700)
	os.WriteFile(filepath.Join(putty, "Mon%20serveur"), []byte("HostName=10.0.0.5\nPortNumber=22\nUserName=root\nProtocol=ssh\n"), 0o600)
	fz := `<?xml version="1.0"?><FileZilla3><Servers><Folder>Partenaires<Server><Host>ftp.example.com</Host><Port>21</Port><Protocol>4</Protocol><User>depot</User><Pass encoding="base64">c2VjcmV0</Pass><Name>Depot</Name></Server></Folder></Servers></FileZilla3>`
	sshcfg := "Host bastion\n  HostName bastion.example.com\n  User alex\n\nHost interne\n  HostName 10.0.0.9\n  User deploy\n  ProxyJump bastion\n  LocalForward 15432 localhost:5432\n"
	var jsonOut string
	e.must(t, "sites.export", map[string]any{"ids": []string{v.ID}, "format": "json"}, &jsonOut)
	cases := []struct {
		source, path, data string
		check              func([]importer.Item) bool
	}{
		{"winscp-ini", "", ini, func(it []importer.Item) bool {
			return len(it) == 1 && it[0].Site.Name == "Serveur web" && it[0].Site.Folder == "Clients" && it[0].Site.RemoteDir == "/config" && it[0].Site.Port == 2222
		}},
		{"winscp-ini", "", exported, func(it []importer.Item) bool { return len(it) == 1 && it[0].Site.Protocol == "ftp" }},
		{"putty", putty, "", func(it []importer.Item) bool {
			return len(it) == 1 && it[0].Site.Name == "Mon serveur" && it[0].Site.User == "root"
		}},
		{"filezilla", "", fz, func(it []importer.Item) bool {
			return len(it) == 1 && it[0].Password == "secret" && it[0].Site.FTPS == "explicit" && it[0].Site.Folder == "Partenaires"
		}},
		{"ssh-config", "", sshcfg, func(it []importer.Item) bool {
			for _, x := range it {
				if x.Site.Name == "interne" {
					return len(x.Site.Jumps) == 1 && len(x.Site.Forwards) == 1 && x.Site.Forwards[0].BindPort == 15432
				}
			}
			return false
		}},
		{"skiff-json", "", jsonOut, func(it []importer.Item) bool { return len(it) == 1 && it[0].Site.Name == "Export test" }},
		{"urls", "", "sftp://a@h1/x\nftpes://b@h2/\n", func(it []importer.Item) bool { return len(it) == 2 }},
	}
	var all []importer.Item
	for _, c := range cases {
		var items []importer.Item
		e.must(t, "sites.importPreview", map[string]string{"source": c.source, "path": c.path, "data": c.data}, &items)
		if !c.check(items) {
			t.Errorf("import %s: %+v", c.source, items)
		}
		all = append(all, items...)
	}
	var n int
	e.must(t, "sites.importCommit", map[string]any{"items": all, "folder": "Importés"}, &n)
	if n != len(all) {
		t.Fatalf("imported %d of %d", n, len(all))
	}
	var list struct {
		Sites []config.SiteView `json:"sites"`
	}
	e.must(t, "sites.list", nil, &list)
	var depot config.SiteView
	var del []string
	for _, x := range list.Sites {
		if strings.HasPrefix(x.Folder, "Importés") {
			del = append(del, x.ID)
			if x.Name == "Depot" {
				depot = x
			}
		}
	}
	if len(del) != n || depot.Folder != "Importés/Partenaires" || !depot.HasPassword {
		t.Fatalf("imported sites: %d, depot = %+v", len(del), depot)
	}
	e.must(t, "sites.delete", map[string]any{"ids": append(del, v.ID)}, nil)
}

func testSecurity(t *testing.T, e *e2e) {
	v := e.saveSite(t, testSite("sftp"), "skiff")
	if _, err := os.Stat(filepath.Join(e.cfg, "secret.key")); err != nil {
		t.Fatal("secret.key missing without a master password")
	}
	e.must(t, "security.setMaster", map[string]string{"old": "", "new": "Maitre-2026!"}, nil)
	var info map[string]any
	e.must(t, "app.info", nil, &info)
	if info["hasMaster"] != true || info["locked"] != false {
		t.Fatalf("after setMaster: %v", info)
	}
	ci := e.open(t, v.ID) // secrets re-encrypted with the master key
	e.must(t, "conn.close", map[string]string{"conn": ci.ID}, nil)
	e.must(t, "security.lock", nil, nil)
	e.must(t, "app.info", nil, &info)
	if info["locked"] != true {
		t.Fatal("not locked")
	}
	if err := e.call("conn.open", map[string]any{"siteId": v.ID}, nil); err == nil || !strings.Contains(err.Error(), "master password") {
		t.Fatalf("locked passwords must not be usable: %v", err)
	}
	e.mustFail(t, "security.unlock", map[string]string{"password": "faux"})
	e.must(t, "security.unlock", map[string]string{"password": "Maitre-2026!"}, nil)
	e.mustFail(t, "security.setMaster", map[string]string{"old": "faux", "new": "x"})
	e.must(t, "security.setMaster", map[string]string{"old": "Maitre-2026!", "new": "Nouveau-2027!"}, nil)
	e.must(t, "security.lock", nil, nil)
	e.mustFail(t, "security.unlock", map[string]string{"password": "Maitre-2026!"})
	e.must(t, "security.unlock", map[string]string{"password": "Nouveau-2027!"}, nil)
	ci = e.open(t, v.ID)
	e.must(t, "conn.close", map[string]string{"conn": ci.ID}, nil)
	e.must(t, "security.setMaster", map[string]string{"old": "Nouveau-2027!", "new": ""}, nil)
	e.must(t, "app.info", nil, &info)
	if info["hasMaster"] != false {
		t.Fatal("master password not removed")
	}
	ci = e.open(t, v.ID)
	e.must(t, "conn.close", map[string]string{"conn": ci.ID}, nil)
	e.must(t, "sites.delete", map[string]any{"ids": []string{v.ID}}, nil)
}

func testConnections(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	var conns []core.ConnInfo
	e.must(t, "conn.list", nil, &conns)
	if len(conns) == 0 {
		t.Fatal("conn.list empty")
	}
	var info []vfs.InfoItem
	e.must(t, "conn.serverInfo", map[string]string{"conn": ci.ID}, &info)
	keys := map[string]string{}
	for _, i := range info {
		keys[i.Group+"/"+i.Key] = i.Value
	}
	if !strings.HasPrefix(keys["SSH/Server"], "SSH-2.0-OpenSSH") || keys["SFTP/Protocol version"] != "3" || keys["Session/Connected since"] == "" {
		t.Errorf("serverInfo = %v", keys)
	}
	if _, err := time.Parse(time.RFC3339, keys["Session/Connected since"]); err != nil {
		t.Errorf("connected since is not RFC 3339: %v", err)
	}
	var lines []core.LogLine
	e.must(t, "conn.log", map[string]string{"conn": ci.ID}, &lines)
	joined := ""
	for _, l := range lines {
		joined += l.Msg + "\n"
	}
	if !strings.Contains(joined, "Authenticated") {
		t.Errorf("log = %s", joined)
	}
	var home string
	e.must(t, "fs.home", map[string]string{"conn": ci.ID}, &home)
	if home != "/config" {
		t.Errorf("home = %s", home)
	}
	var rc core.ConnInfo
	e.must(t, "conn.reconnect", map[string]string{"conn": ci.ID}, &rc)
	if rc.State != "connected" {
		t.Fatalf("reconnect: %+v", rc)
	}
	// abort a connection attempt to an unreachable host
	s := testSite("sftp")
	s.Host, s.Timeout = "10.255.255.1", 60
	errc := make(chan error, 1)
	go func() {
		errc <- e.call("conn.open", map[string]any{"site": s, "password": "x", "token": "abort-me"}, nil)
	}()
	time.Sleep(800 * time.Millisecond)
	start := time.Now()
	e.must(t, "conn.abort", map[string]string{"token": "abort-me"}, nil)
	select {
	case err := <-errc:
		if err == nil || time.Since(start) > 5*time.Second {
			t.Errorf("abort: %v after %v", err, time.Since(start))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("abort did not stop the connection attempt")
	}
	// lost connection and automatic reconnection
	if _, err := exec.LookPath("docker"); err == nil {
		root := e.remoteRoot(t, ci)
		if out, err := exec.Command("docker", "restart", "-t", "1", "testenv-ssh-1").CombinedOutput(); err != nil {
			t.Fatalf("docker restart: %v %s", err, out)
		}
		eventually(t, "conn lost", 20*time.Second, func() bool {
			e.must(t, "conn.list", nil, &conns)
			for _, c := range conns {
				if c.ID == ci.ID {
					return c.State == "lost"
				}
			}
			return false
		})
		eventually(t, "sshd back", 30*time.Second, func() bool {
			return e.call("fs.list", map[string]string{"conn": ci.ID, "path": root}, nil) == nil
		})
		e.must(t, "conn.list", nil, &conns)
		for _, c := range conns {
			if c.ID == ci.ID && c.State != "connected" {
				t.Errorf("after auto-reconnect: %s", c.State)
			}
		}
	}
	e.must(t, "conn.close", map[string]string{"conn": ci.ID}, nil)
	e.must(t, "conn.list", nil, &conns)
	for _, c := range conns {
		if c.ID == ci.ID {
			t.Fatal("closed connection still listed")
		}
	}
}
