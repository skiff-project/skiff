package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"skiff/internal/vfs"
)

// Shell is a shell or an interpreter a terminal can run.
type Shell struct {
	ID      string   `json:"id"`   // bash, pwsh, python3, wsl:Ubuntu…
	Name    string   `json:"name"` // label
	Kind    string   `json:"kind"` // shell, repl
	Path    string   `json:"path"`
	Args    []string `json:"args,omitempty"`
	Default bool     `json:"default,omitempty"`
}

// candidate is a known program a terminal may run.
type candidate struct {
	id, name, kind string
	names          []string // executables, the first found wins
	args           []string
}

// unixCandidates, in the order shown.
var unixCandidates = []candidate{
	{"bash", "Bash", "shell", []string{"bash"}, []string{"-l"}},
	{"zsh", "Zsh", "shell", []string{"zsh"}, []string{"-l"}},
	{"fish", "Fish", "shell", []string{"fish"}, []string{"-l"}},
	{"nu", "Nushell", "shell", []string{"nu"}, []string{"-l"}},
	{"ksh", "Korn shell", "shell", []string{"ksh", "mksh"}, []string{"-l"}},
	{"pwsh", "PowerShell", "shell", []string{"pwsh"}, []string{"-NoLogo"}},
	{"sh", "sh", "shell", []string{"sh"}, []string{"-l"}},
	{"python", "Python", "repl", []string{"python3", "python"}, nil},
	{"ipython", "IPython", "repl", []string{"ipython3", "ipython"}, nil},
	{"node", "Node.js", "repl", []string{"node"}, nil},
	{"deno", "Deno", "repl", []string{"deno"}, nil},
	{"bun", "Bun", "repl", []string{"bun"}, []string{"repl"}},
	{"irb", "Ruby (irb)", "repl", []string{"irb"}, nil},
	{"php", "PHP", "repl", []string{"php"}, []string{"-a"}},
	{"lua", "Lua", "repl", []string{"lua", "lua5.4", "lua5.3"}, nil},
	{"R", "R", "repl", []string{"R"}, []string{"--quiet"}},
}

// windowsCandidates come before the WSL distributions.
var windowsCandidates = []candidate{
	{"pwsh", "PowerShell 7", "shell", []string{"pwsh"}, []string{"-NoLogo"}},
	{"powershell", "Windows PowerShell", "shell", []string{"powershell"}, []string{"-NoLogo"}},
	{"cmd", "Command Prompt", "shell", []string{"cmd"}, nil},
	{"python", "Python", "repl", []string{"py", "python"}, nil},
	{"ipython", "IPython", "repl", []string{"ipython"}, nil},
	{"node", "Node.js", "repl", []string{"node"}, nil},
	{"deno", "Deno", "repl", []string{"deno"}, nil},
	{"bun", "Bun", "repl", []string{"bun"}, []string{"repl"}},
	{"irb", "Ruby (irb)", "repl", []string{"irb"}, nil},
	{"php", "PHP", "repl", []string{"php"}, []string{"-a"}},
	{"lua", "Lua", "repl", []string{"lua"}, nil},
}

// lookPath finds a program, also in the usual folders of Windows.
var lookPath = func(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles")
		for _, p := range map[string][]string{
			"pwsh": {filepath.Join(pf, "PowerShell", "7", "pwsh.exe")},
			"bash": {filepath.Join(pf, "Git", "bin", "bash.exe")},
		}[name] {
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
	}
	return "", false
}

// localShells lists what this computer can run in a terminal.
func localShells() []Shell {
	var out []Shell
	add := func(cands []candidate) {
		for _, c := range cands {
			for _, n := range c.names {
				if p, ok := lookPath(n); ok {
					out = append(out, Shell{ID: c.id, Name: c.name, Kind: c.kind, Path: p, Args: c.args})
					break
				}
			}
		}
	}
	if runtime.GOOS == "windows" {
		add(windowsCandidates)
		if p, ok := lookPath("bash"); ok && strings.Contains(strings.ToLower(p), `\git\`) {
			out = append(out, Shell{ID: "gitbash", Name: "Git Bash", Kind: "shell", Path: p, Args: []string{"--login", "-i"}})
		}
		out = append(out, wslDistros()...)
		markDefault(out, "pwsh", "powershell", "cmd")
		return out
	}
	add(unixCandidates)
	// the login shell of the user first
	if sh := os.Getenv("SHELL"); sh != "" {
		markDefaultPath(out, sh)
	}
	markDefault(out, "bash", "sh")
	return out
}

// wslDistros lists the WSL distributions ("wsl -l -q" writes UTF-16).
func wslDistros() []Shell {
	p, ok := lookPath("wsl")
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, p, "-l", "-q").Output()
	if err != nil {
		return nil
	}
	var out []Shell
	for _, name := range strings.Split(decodeUTF16(b), "\n") {
		name = strings.TrimSpace(strings.Trim(name, "\x00\r"))
		if name == "" {
			continue
		}
		out = append(out, Shell{ID: "wsl:" + name, Name: name + " (WSL)", Kind: "shell", Path: p, Args: []string{"-d", name}})
	}
	return out
}

func decodeUTF16(b []byte) string {
	if len(b) < 2 || b[1] != 0 { // already 8-bit
		return string(b)
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

func markDefault(list []Shell, ids ...string) {
	for _, s := range list {
		if s.Default {
			return
		}
	}
	for _, id := range ids {
		for i := range list {
			if list[i].ID == id {
				list[i].Default = true
				return
			}
		}
	}
}

func markDefaultPath(list []Shell, path string) {
	base := filepath.Base(path)
	for i := range list {
		if list[i].Kind == "shell" && (list[i].Path == path || filepath.Base(list[i].Path) == base) {
			list[i].Default = true
			return
		}
	}
}

// remoteScript finds the candidates on a POSIX server or container.
func remoteScript() string {
	var b strings.Builder
	b.WriteString(`echo "SHELL=$SHELL"; for c in`)
	seen := map[string]bool{}
	for _, c := range unixCandidates {
		for _, n := range c.names {
			if !seen[n] {
				seen[n] = true
				b.WriteString(" " + n)
			}
		}
	}
	b.WriteString(`; do p=$(command -v "$c" 2>/dev/null) && echo "$c=$p"; done; true`)
	return b.String()
}

// parseRemoteShells reads the output of remoteScript.
func parseRemoteShells(out string) []Shell {
	found := map[string]string{}
	login := ""
	for _, ln := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(ln), "=")
		if !ok {
			continue
		}
		if k == "SHELL" {
			login = v
		} else if strings.HasPrefix(v, "/") {
			found[k] = v
		}
	}
	var list []Shell
	for _, c := range unixCandidates {
		for _, n := range c.names {
			if p, ok := found[n]; ok {
				list = append(list, Shell{ID: c.id, Name: c.name, Kind: c.kind, Path: p, Args: c.args})
				break
			}
		}
	}
	if login != "" {
		markDefaultPath(list, login)
	}
	markDefault(list, "bash", "sh")
	return list
}

type shellCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	list map[string][]Shell
}

// Shells lists the shells and interpreters a terminal can run on a
// connection ("local" for this computer). The list is kept a minute.
func (c *Core) Shells(ctx context.Context, conn string) ([]Shell, error) {
	sc := &c.shells
	sc.mu.Lock()
	if sc.list == nil {
		sc.list, sc.at = map[string][]Shell{}, map[string]time.Time{}
	}
	if l, ok := sc.list[conn]; ok && time.Since(sc.at[conn]) < time.Minute {
		sc.mu.Unlock()
		return l, nil
	}
	sc.mu.Unlock()
	var list []Shell
	if conn == LocalID {
		list = localShells()
	} else {
		cn, err := c.Conn(conn)
		if err != nil {
			return nil, err
		}
		if cn.SSH() == nil && cn.container == nil {
			return nil, errors.New("terminals are only available for SFTP, SCP and container connections")
		}
		out, err := cn.sshOutput(ctx, remoteScript())
		if err != nil {
			// not a POSIX shell (a Windows server…): only its own shell
			list = []Shell{{ID: "", Name: "Shell", Kind: "shell", Default: true}}
		} else {
			list = parseRemoteShells(out)
		}
	}
	if list == nil {
		list = []Shell{}
	}
	sc.mu.Lock()
	sc.list[conn], sc.at[conn] = list, time.Now()
	sc.mu.Unlock()
	return list, nil
}

// shellFor resolves the shell a terminal asks for ("" = the default one).
func (c *Core) shellFor(ctx context.Context, conn, id string) (*Shell, error) {
	if id == "" && conn == LocalID {
		id = c.Store.Prefs().Terminal.LocalShell
	}
	if id == "" {
		return nil, nil // the login shell
	}
	list, err := c.Shells(ctx, conn)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			if list[i].Path == "" {
				return nil, nil
			}
			return &list[i], nil
		}
	}
	if conn == LocalID {
		return nil, nil // the saved default is not installed any more
	}
	return nil, errors.New("this program is not installed there: " + id)
}

// commandLine is the shell command that runs sh in cwd.
func (s *Shell) commandLine(cwd string) string {
	q := []string{"exec", vfs.ShellQuote(s.Path)}
	for _, a := range s.Args {
		q = append(q, vfs.ShellQuote(a))
	}
	line := strings.Join(q, " ")
	if cwd != "" {
		line = "cd " + vfs.ShellQuote(cwd) + " 2>/dev/null; " + line
	}
	return line
}
