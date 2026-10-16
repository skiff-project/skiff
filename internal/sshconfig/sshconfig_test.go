package sshconfig

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testConfig writes the user and system files under a fake home.
func testConfig(t *testing.T, userCfg, systemCfg string) *Config {
	t.Helper()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	sys := filepath.Join(t.TempDir(), "ssh_config")
	u := filepath.Join(home, ".ssh", "config")
	os.WriteFile(u, []byte(userCfg), 0o600)
	if systemCfg != "" {
		os.WriteFile(sys, []byte(systemCfg), 0o644)
	}
	return &Config{UserFile: u, SystemFile: sys, Home: home, LocalUser: "sam", LocalHost: "portable.lan",
		Getenv: func(k string) string { return map[string]string{"KEYS": "/keys", "SOCK": "/run/agent.sock"}[k] }}
}

func TestPaths(t *testing.T) {
	u, s := paths("linux", "/home/sam", func(string) string { return "" })
	if u != "/home/sam/.ssh/config" || s != "/etc/ssh/ssh_config" {
		t.Errorf("linux: %s %s", u, s)
	}
	u, s = paths("darwin", "/Users/sam", func(string) string { return "" })
	if u != "/Users/sam/.ssh/config" || s != "/etc/ssh/ssh_config" {
		t.Errorf("darwin: %s %s", u, s)
	}
	u, s = paths("windows", `C:\Users\sam`, func(k string) string { return map[string]string{"ProgramData": `D:\ProgramData`}[k] })
	if u != `C:\Users\sam\.ssh\config` || s != `D:\ProgramData\ssh\ssh_config` {
		t.Errorf("windows: %s %s", u, s)
	}
	if _, s = paths("windows", `C:\Users\sam`, func(string) string { return "" }); s != `C:\ProgramData\ssh\ssh_config` {
		t.Errorf("windows default ProgramData: %s", s)
	}
}

func TestSplitLine(t *testing.T) {
	for _, tc := range []struct {
		in   string
		key  string
		args []string
	}{
		{"  HostName example.com", "hostname", []string{"example.com"}},
		{"Port=2222", "port", []string{"2222"}},
		{"Port = 2222", "port", []string{"2222"}},
		{`IdentityFile "~/My Keys/id rsa"`, "identityfile", []string{"~/My Keys/id rsa"}},
		{`IdentityFile 'C:\Users\sam\.ssh\id_ed25519'`, "identityfile", []string{`C:\Users\sam\.ssh\id_ed25519`}},
		{`IdentityFile C:\Users\sam\.ssh\id_ed25519`, "identityfile", []string{`C:\Users\sam\.ssh\id_ed25519`}},
		{`IdentityFile ~/My\ Keys/id`, "identityfile", []string{"~/My Keys/id"}},
		{`ProxyCommand ssh -W %h:%p "jump host"  # via the bastion`, "proxycommand", []string{"ssh", "-W", "%h:%p", "jump host"}},
		{"# comment", "", nil},
		{"", "", nil},
	} {
		key, _, args, err := splitLine(tc.in)
		if err != nil || key != tc.key || !reflect.DeepEqual(args, tc.args) {
			t.Errorf("%q: got %q %q %v", tc.in, key, args, err)
		}
	}
	if _, _, _, err := splitLine(`HostName "unterminated`); err == nil {
		t.Error("unterminated quote accepted")
	}
}

func TestWildcard(t *testing.T) {
	for _, tc := range []struct {
		p, s string
		ok   bool
	}{
		{"*", "anything", true}, {"*.example.com", "www.example.com", true}, {"*.example.com", "example.com", false},
		{"web?", "web1", true}, {"web?", "web12", false}, {"10.0.*.*", "10.0.3.4", true}, {"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
	} {
		if wildcard(tc.p, tc.s) != tc.ok {
			t.Errorf("%s ~ %s: want %v", tc.p, tc.s, tc.ok)
		}
	}
	if !matchList("Web1.Example.com", "*.example.com,!db*", true) || matchList("db1.example.com", "*.example.com,!db*", true) {
		t.Error("negated pattern")
	}
}

func TestFirstValueWins(t *testing.T) {
	c := testConfig(t, `
Host prod
    HostName 10.0.0.5
    User deploy
    IdentityFile ~/.ssh/deploy

Host prod staging
    User other
    Port 2222
    IdentityFile ~/.ssh/second

Host *
    User fallback
    ServerAliveInterval 60
    IdentityFile ~/.ssh/deploy
`, "Host *\n    Port 22\n    ConnectTimeout 7\n")
	h := c.Resolve("prod", Query{})
	if h.HostName() != "10.0.0.5" || h.User() != "deploy" || h.Port() != 2222 {
		t.Errorf("got %s %s %d", h.HostName(), h.User(), h.Port())
	}
	want := []string{filepath.Join(c.Home, ".ssh", "deploy"), filepath.Join(c.Home, ".ssh", "second")}
	if got := h.IdentityFiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("identity files %v, want %v (duplicates dropped)", got, want)
	}
	if v, _ := h.Int("ConnectTimeout"); v != 7 {
		t.Error("system file not read")
	}
	if !strings.HasSuffix(h.Options[0].Source, "config:3") || !strings.HasPrefix(h.Options[0].Source, "~") {
		t.Errorf("source %s", h.Options[0].Source)
	}
	o := c.Resolve("unknown.example.com", Query{User: "root", Port: 2200})
	if o.HostName() != "unknown.example.com" || o.User() != "root" || o.Port() != 2200 {
		t.Errorf("query should win: %s %s %d", o.HostName(), o.User(), o.Port())
	}
	if n := c.Resolve("nothing", Query{}); n.User() != "fallback" {
		t.Errorf("Host * default user %q", n.User())
	}
	empty := testConfig(t, "", "")
	if u := empty.Resolve("x", Query{}).User(); u != "sam" {
		t.Errorf("local user default %q", u)
	}
}

func TestHostNameToken(t *testing.T) {
	c := testConfig(t, "Host *.lan\n    HostName %h.example.com\n", "")
	if h := c.Resolve("nas.lan", Query{}); h.HostName() != "nas.lan.example.com" {
		t.Error(h.HostName())
	}
}

func TestInclude(t *testing.T) {
	c := testConfig(t, "Include config.d/*\nHost work\n    Include work.conf\nHost other\n    User o\n", "")
	d := filepath.Join(c.Home, ".ssh", "config.d")
	os.MkdirAll(d, 0o700)
	os.WriteFile(filepath.Join(d, "10-nas"), []byte("Host nas\n    HostName 192.168.1.20\n    Port 2200\n"), 0o600)
	os.WriteFile(filepath.Join(d, "20-git"), []byte("Host git\n    User git\n"), 0o600)
	// a conditional include only applies inside its block
	os.WriteFile(filepath.Join(c.Home, ".ssh", "work.conf"), []byte("User worker\nPort 2022\n"), 0o600)
	if h := c.Resolve("nas", Query{}); h.HostName() != "192.168.1.20" || h.Port() != 2200 {
		t.Errorf("nas %s %d", h.HostName(), h.Port())
	}
	if h := c.Resolve("work", Query{}); h.User() != "worker" || h.Port() != 2022 {
		t.Errorf("work %s %d", h.User(), h.Port())
	}
	if h := c.Resolve("other", Query{}); h.User() != "o" || h.Port() != 22 {
		t.Errorf("Host lines of the included file leaked: %s %d", h.User(), h.Port())
	}
	var aliases []string
	for _, e := range c.Hosts() {
		aliases = append(aliases, e.Alias)
	}
	if !reflect.DeepEqual(aliases, []string{"nas", "git", "work", "other"}) {
		t.Errorf("hosts %v", aliases)
	}
	files := c.Files()
	if len(files) != 5 || files[0].Scope != "user" || !files[0].Exists || files[1].Scope != "system" || files[2].Scope != "include" {
		t.Errorf("files %+v", files)
	}
}

func TestMatch(t *testing.T) {
	c := testConfig(t, `
Host bastion
    HostName bastion.example.com
Match originalhost bastion user admin
    Port 2201
Match host *.example.com !localuser root
    User fromhost
Match exec "test %h = bastion.example.com"
    IdentityFile ~/.ssh/exec_key
Match localnetwork 192.168.1.0/24
    ProxyJump none
Match tagged work
    Port 2300
Match final host bastion.example.com
    ForwardAgent yes
Match all
    Compression yes
`, "")
	var ran []string
	c.Exec = func(cmd string) bool {
		ran = append(ran, cmd)
		return cmd == "test bastion.example.com = bastion.example.com"
	}
	c.LocalAddrs = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.42")} }
	h := c.Resolve("bastion", Query{User: "admin"})
	if p, _ := h.Int("Port"); p != 2201 {
		t.Errorf("Match originalhost/user: port %d", p)
	}
	if h.Value("User") != "fromhost" {
		t.Errorf("Match host sees the HostName: %q", h.Value("User"))
	}
	if len(h.IdentityFiles()) != 1 || len(ran) == 0 {
		t.Errorf("Match exec: %v %v", h.IdentityFiles(), ran)
	}
	if h.Value("ProxyJump") != "none" {
		t.Error("Match localnetwork")
	}
	if v, _ := h.Bool("ForwardAgent"); !v {
		t.Error("Match final")
	}
	if v, _ := h.Bool("Compression"); !v {
		t.Error("Match all")
	}
	// tagged
	c2 := testConfig(t, "Tag work\nMatch tagged work\n    Port 2300\n", "")
	if c2.Resolve("x", Query{}).Port() != 2300 {
		t.Error("Match tagged")
	}
	// an unknown criterion never matches and is reported
	c3 := testConfig(t, "Match sessiontype subsystem\n    Port 1\n", "")
	h3 := c3.Resolve("x", Query{})
	if h3.Port() != 22 || len(h3.Warnings) == 0 {
		t.Errorf("unknown criterion: %d %v", h3.Port(), h3.Warnings)
	}
	// exec is not run when the host name carries shell syntax
	ran = nil
	h4 := c.Resolve("bastion;touch /tmp/x", Query{})
	if len(ran) != 0 || len(h4.Warnings) == 0 {
		t.Errorf("unsafe exec ran: %v", ran)
	}
}

func TestTokensAndPaths(t *testing.T) {
	c := testConfig(t, `
Host box
    HostName box.example.com
    User alice
    Port 2022
    IdentityFile ~/.ssh/%r@%h-%p
    IdentityFile ${KEYS}/%n
    CertificateFile %d/.ssh/%u-cert.pub
    IdentityAgent $SOCK
    UserKnownHostsFile ~/.ssh/known_%h /dev/null
    ProxyCommand ssh -W %h:%p -l %r jump # %% literal: 100%%
    HostKeyAlias box-key
`, "")
	h := c.Resolve("box", Query{})
	ids := h.IdentityFiles()
	if ids[0] != filepath.Join(c.Home, ".ssh", "alice@box.example.com-2022") || ids[1] != "/keys/box" {
		t.Errorf("identity files %v", ids)
	}
	if cf := h.CertificateFiles(); cf[0] != c.Home+"/.ssh/sam-cert.pub" {
		t.Errorf("cert %v", cf)
	}
	if p, off := h.IdentityAgent(); p != "/run/agent.sock" || off {
		t.Errorf("agent %s %v", p, off)
	}
	if f, set := h.KnownHostsFiles(); !set || len(f) != 1 || f[0] != filepath.Join(c.Home, ".ssh", "known_box.example.com") {
		t.Errorf("known hosts %v %v", f, set)
	}
	cmd, err := h.ProxyCommand()
	if err != nil || cmd != "ssh -W box.example.com:2022 -l alice jump" {
		t.Errorf("proxy command %q %v", cmd, err)
	}
	if tok := h.tokens("box.example.com"); tok['k'] != "box-key" || tok['L'] != "portable" || len(tok['C']) != 40 {
		t.Errorf("tokens %v", tok)
	}
	if _, err := c.Resolve("box", Query{User: "x$(id)"}).ProxyCommand(); err == nil {
		t.Error("unsafe user accepted in ProxyCommand")
	}
	c2 := testConfig(t, "Host *\n    IdentityAgent none\n", "")
	if _, off := c2.Resolve("a", Query{}).IdentityAgent(); !off {
		t.Error("IdentityAgent none")
	}
}

func TestProxyJumpAndCommandCompete(t *testing.T) {
	c := testConfig(t, "Host a\n    ProxyJump j1,admin@[2001:db8::1]:2200,ssh://j3:2222\n    ProxyCommand nc %h %p\nHost b\n    ProxyCommand nc %h %p\n    ProxyJump j1\n", "")
	a := c.Resolve("a", Query{})
	if a.Has("ProxyCommand") {
		t.Error("ProxyCommand kept after ProxyJump")
	}
	hops := a.ProxyJump()
	want := []Hop{{Host: "j1"}, {User: "admin", Host: "2001:db8::1", Port: 2200}, {Host: "j3", Port: 2222}}
	if !reflect.DeepEqual(hops, want) {
		t.Errorf("hops %+v", hops)
	}
	if hops[1].String() != "admin@[2001:db8::1]:2200" {
		t.Error(hops[1].String())
	}
	if b := c.Resolve("b", Query{}); b.Has("ProxyJump") || b.Value("ProxyCommand") == "" {
		t.Error("ProxyJump kept after ProxyCommand")
	}
}

func TestForwards(t *testing.T) {
	c := testConfig(t, "Host db\n    LocalForward 5432 db.internal:5432\n    LocalForward 127.0.0.1:8080 [::1]:80\n    RemoteForward 9000 localhost:9000\n    DynamicForward 1080\n    LocalForward /tmp/sock /run/x.sock\n", "")
	got := c.Resolve("db", Query{}).Forwards()
	if len(got) != 4 || got[0].DestHost != "db.internal" || got[1].BindHost != "127.0.0.1" || got[1].DestHost != "::1" ||
		got[2].Type != "remote" || got[3].Type != "dynamic" || got[3].BindPort != 1080 {
		t.Errorf("forwards %+v", got)
	}
}

func TestAlgorithms(t *testing.T) {
	def := []string{"a", "b", "c"}
	sup := []string{"a", "b", "c", "d-old", "e-old"}
	for _, tc := range []struct {
		spec string
		want []string
	}{
		{"b,a,zz", []string{"b", "a"}}, {"+d-old", []string{"a", "b", "c", "d-old"}}, {"+*-old", []string{"a", "b", "c", "d-old", "e-old"}},
		{"-b", []string{"a", "c"}}, {"^c", []string{"c", "a", "b"}}, {"", nil},
	} {
		if got := Algorithms(tc.spec, def, sup); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %v, want %v", tc.spec, got, tc.want)
		}
	}
}
