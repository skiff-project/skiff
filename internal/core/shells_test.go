package core

import (
	"strings"
	"testing"
)

func TestParseRemoteShells(t *testing.T) {
	out := "SHELL=/usr/bin/zsh\nbash=/bin/bash\nzsh=/usr/bin/zsh\nsh=/bin/sh\npython3=/usr/bin/python3\npython=/usr/bin/python\nnode=/usr/local/bin/node\nphp=alias php='php8'\n"
	list := parseRemoteShells(out)
	ids := []string{}
	var def string
	for _, s := range list {
		ids = append(ids, s.ID)
		if s.Default {
			def = s.ID
		}
	}
	if strings.Join(ids, ",") != "bash,zsh,sh,python,node" || def != "zsh" {
		t.Errorf("shells %v, default %s", ids, def)
	}
	if list[3].Path != "/usr/bin/python3" || list[3].Kind != "repl" {
		t.Errorf("python3 is preferred: %+v", list[3])
	}
	if l := parseRemoteShells("SHELL=/bin/ash\nsh=/bin/sh\n"); len(l) != 1 || !l[0].Default {
		t.Errorf("fallback default %+v", l)
	}
}

func TestShellCommandLine(t *testing.T) {
	s := Shell{Path: "/usr/bin/python3"}
	if got := s.commandLine("/srv/my app"); got != "cd '/srv/my app' 2>/dev/null; exec '/usr/bin/python3'" && got != "cd '/srv/my app' 2>/dev/null; exec /usr/bin/python3" {
		t.Errorf("command line %q", got)
	}
	b := Shell{Path: "/bin/bash", Args: []string{"-l"}}
	if got := b.commandLine(""); !strings.HasPrefix(got, "exec ") || !strings.HasSuffix(got, "-l") {
		t.Errorf("command line %q", got)
	}
	if !strings.Contains(remoteScript(), "python3 python") || !strings.Contains(remoteScript(), `command -v "$c"`) {
		t.Error(remoteScript())
	}
}

func TestLocalShellsUsesPath(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	have := map[string]string{"bash": "/bin/bash", "node": "/opt/node/bin/node", "python": "/usr/bin/python"}
	lookPath = func(n string) (string, bool) { p, ok := have[n]; return p, ok }
	t.Setenv("SHELL", "/bin/bash")
	list := localShells()
	if len(list) != 3 || list[0].ID != "bash" || !list[0].Default || list[1].ID != "python" || list[2].Name != "Node.js" {
		t.Errorf("%+v", list)
	}
}

func TestDecodeUTF16(t *testing.T) {
	// "Ubuntu\r\n" as written by wsl -l -q
	b := []byte{'U', 0, 'b', 0, 'u', 0, 'n', 0, 't', 0, 'u', 0, '\r', 0, '\n', 0}
	if got := decodeUTF16(b); got != "Ubuntu\r\n" {
		t.Errorf("%q", got)
	}
}
