// Package sshconfig reads the OpenSSH client configuration the way ssh(1)
// does, so that Skiff reaches a host with the settings the ssh command would
// use: the user file then the system file (where they live depends on the
// OS), Host and Match blocks, Include, "the first obtained value wins",
// %-tokens, ${VAR} and ~ expansion.
package sshconfig

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Paths returns the files ssh reads on this OS: the user's ~/.ssh/config
// (%USERPROFILE%\.ssh\config on Windows) and the system-wide ssh_config
// (/etc/ssh/ssh_config, or %ProgramData%\ssh\ssh_config on Windows).
//
// SKIFF_SSH_SYSTEM_CONFIG replaces the system file ("none" skips it).
func Paths() (userFile, systemFile string) {
	h, _ := os.UserHomeDir()
	userFile, systemFile = paths(runtime.GOOS, h, os.Getenv)
	if v := os.Getenv("SKIFF_SSH_SYSTEM_CONFIG"); v == "none" {
		systemFile = ""
	} else if v != "" {
		systemFile = v
	}
	return userFile, systemFile
}

func paths(goos, home string, getenv func(string) string) (userFile, systemFile string) {
	if goos == "windows" {
		pd := getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return home + `\.ssh\config`, pd + `\ssh\ssh_config`
	}
	return home + "/.ssh/config", "/etc/ssh/ssh_config"
}

// Config is a set of configuration files and the local environment used to
// evaluate them.
type Config struct {
	UserFile   string // read first; relative Include paths are under ~/.ssh
	SystemFile string // read next; relative Include paths are next to it
	// UserData, when set, is used as the content of UserFile.
	UserData []byte

	Home      string
	LocalUser string
	LocalHost string
	Getenv    func(string) string
	// Exec runs a "Match exec" command and reports whether it succeeded.
	Exec func(command string) bool
	// LocalAddrs lists the addresses of this computer ("Match localnetwork").
	LocalAddrs func() []net.IP
}

// New returns a configuration reading the given files ("" = none).
func New(userFile, systemFile string) *Config {
	c := &Config{UserFile: userFile, SystemFile: systemFile, Getenv: os.Getenv, Exec: runShell, LocalAddrs: localAddrs}
	c.Home, _ = os.UserHomeDir()
	c.LocalUser = localUser()
	c.LocalHost, _ = os.Hostname()
	return c
}

// Default reads the user and system files of this OS.
func Default() *Config {
	u, s := Paths()
	return New(u, s)
}

// LocalUser is the user name ssh uses when none is given.
func LocalUser() string { return localUser() }

func localUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		name := u.Username
		if i := strings.LastIndexAny(name, `\`); i >= 0 { // DOMAIN\name on Windows
			name = name[i+1:]
		}
		return name
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return os.Getenv("USERNAME")
}

func runShell(command string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	return cmd.Run() == nil
}

func localAddrs() []net.IP {
	addrs, _ := net.InterfaceAddrs()
	var out []net.IP
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP)
		}
	}
	return out
}

func (c *Config) userIncludeDir() string { return filepath.Join(c.Home, ".ssh") }

// File is a configuration file Skiff looked for.
type File struct {
	Path   string `json:"path"`
	Scope  string `json:"scope"` // user, system, include
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
}

// Option is a value obtained for a host.
type Option struct {
	Key    string `json:"key"`    // keyword, e.g. "IdentityFile"
	Value  string `json:"value"`  // arguments as written
	Source string `json:"source"` // file:line
}

// HostEntry is a host name declared by a Host line of the user's files.
type HostEntry struct {
	Alias  string `json:"alias"`
	Source string `json:"source"`
}

// Query gives what is already known about the connection: the user and
// port set elsewhere win over the configuration, as on the ssh command line.
type Query struct {
	User string
	Port int
}

// keywords that may be given several times (all values are kept).
var multi = map[string]bool{
	"identityfile": true, "certificatefile": true, "localforward": true, "remoteforward": true,
	"dynamicforward": true, "sendenv": true, "setenv": true,
}

// canonical spellings for display.
var canonical = map[string]string{}

func init() {
	for _, k := range []string{
		"AddKeysToAgent", "AddressFamily", "BatchMode", "BindAddress", "BindInterface", "CanonicalDomains",
		"CanonicalizeFallbackLocal", "CanonicalizeHostname", "CanonicalizeMaxDots", "CanonicalizePermittedCNAMEs",
		"CASignatureAlgorithms", "CertificateFile", "ChallengeResponseAuthentication", "ChannelTimeout", "CheckHostIP",
		"Ciphers", "ClearAllForwardings", "Compression", "ConnectionAttempts", "ConnectTimeout", "ControlMaster",
		"ControlPath", "ControlPersist", "DynamicForward", "EnableEscapeCommandline", "EnableSSHKeysign", "EscapeChar",
		"ExitOnForwardFailure", "FingerprintHash", "ForkAfterAuthentication", "ForwardAgent", "ForwardX11",
		"ForwardX11Timeout", "ForwardX11Trusted", "GatewayPorts", "GlobalKnownHostsFile", "GSSAPIAuthentication",
		"GSSAPIDelegateCredentials", "HashKnownHosts", "HostbasedAcceptedAlgorithms", "HostbasedAuthentication",
		"HostKeyAlgorithms", "HostKeyAlias", "HostName", "IdentitiesOnly", "IdentityAgent", "IdentityFile",
		"IgnoreUnknown", "IPQoS", "KbdInteractiveAuthentication", "KbdInteractiveDevices", "KexAlgorithms",
		"KnownHostsCommand", "LocalCommand", "LocalForward", "LogLevel", "LogVerbose", "MACs", "NoHostAuthenticationForLocalhost",
		"NumberOfPasswordPrompts", "ObscureKeystrokeTiming", "PasswordAuthentication", "PermitLocalCommand",
		"PermitRemoteOpen", "PKCS11Provider", "Port", "PreferredAuthentications", "ProxyCommand", "ProxyJump",
		"ProxyUseFdpass", "PubkeyAcceptedAlgorithms", "PubkeyAcceptedKeyTypes", "PubkeyAuthentication", "RekeyLimit",
		"RemoteCommand", "RemoteForward", "RequestTTY", "RequiredRSASize", "RevokedHostKeys", "SecurityKeyProvider",
		"SendEnv", "ServerAliveCountMax", "ServerAliveInterval", "SessionType", "SetEnv", "StdinNull",
		"StreamLocalBindMask", "StreamLocalBindUnlink", "StrictHostKeyChecking", "Tag", "TCPKeepAlive", "Tunnel",
		"TunnelDevice", "UpdateHostKeys", "UseKeychain", "User", "UserKnownHostsFile", "VerifyHostKeyDNS",
		"VisualHostKey", "XAuthLocation",
	} {
		canonical[strings.ToLower(k)] = k
	}
}

// splitLine returns the lower-cased keyword, the keyword as written and the
// arguments of a configuration line (OpenSSH quoting rules).
func splitLine(ln string) (key, written string, args []string, err error) {
	s := strings.TrimSpace(ln)
	if s == "" || s[0] == '#' {
		return "", "", nil, nil
	}
	i := strings.IndexAny(s, " \t=")
	if i < 0 {
		return strings.ToLower(s), s, nil, nil
	}
	written = s[:i]
	rest := strings.TrimLeft(s[i:], " \t")
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	args, err = splitArgs(rest)
	return strings.ToLower(written), written, args, err
}

// splitArgs splits on blanks, honouring single and double quotes; a
// backslash only escapes a quote, a backslash or (unquoted) a blank, so
// Windows paths keep theirs. An unquoted # starts a comment.
func splitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	have := false
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\\' && i+1 < len(s) {
			n := s[i+1]
			if n == '\'' || n == '"' || n == '\\' || (quote == 0 && (n == ' ' || n == '\t')) {
				cur.WriteByte(n)
				have = true
				i++
				continue
			}
		}
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			quote, have = ch, true
		case ch == ' ' || ch == '\t':
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		case ch == '#' && !have:
			return out, nil
		default:
			cur.WriteByte(ch)
			have = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if have {
		out = append(out, cur.String())
	}
	return out, nil
}

// wildcard matches s against an OpenSSH pattern (* and ?).
func wildcard(p, s string) bool {
	px, sx, star, mark := 0, 0, -1, 0
	for sx < len(s) {
		switch {
		case px < len(p) && (p[px] == '?' || p[px] == s[sx]):
			px++
			sx++
		case px < len(p) && p[px] == '*':
			star, mark = px, sx
			px++
		case star >= 0:
			px = star + 1
			mark++
			sx = mark
		default:
			return false
		}
	}
	for px < len(p) && p[px] == '*' {
		px++
	}
	return px == len(p)
}

// matchList matches s against a comma-separated pattern list: a negated
// pattern that matches rejects s whatever the others say.
func matchList(s, list string, fold bool) bool {
	if fold {
		s = strings.ToLower(s)
	}
	got := false
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if fold {
			p = strings.ToLower(p)
		}
		neg := strings.HasPrefix(p, "!")
		if neg {
			p = p[1:]
		}
		if p != "" && wildcard(p, s) {
			if neg {
				return false
			}
			got = true
		}
	}
	return got
}

// display shortens a path under the home directory to ~.
func (c *Config) display(p string) string {
	if c.Home != "" && strings.HasPrefix(p, c.Home+string(filepath.Separator)) {
		return "~" + p[len(c.Home):]
	}
	return p
}

// walker reads a file and the files it includes.
type walker struct {
	c     *Config
	warn  func(string)
	files map[string]bool // every file read, to report them
	// visit handles one line; active tells whether the enclosing block
	// applies and is updated by Host and Match lines.
	visit func(active *bool, key, written string, args []string, src string)
}

func (w *walker) read(path, incDir string, depth int) {
	if depth > 16 {
		w.warn(fmt.Sprintf("%s: too many nested Include", w.c.display(path)))
		return
	}
	data, err := w.c.UserData, error(nil)
	if path != w.c.UserFile || data == nil {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			w.warn(fmt.Sprintf("%s: %v", w.c.display(path), err))
		}
		return
	}
	if w.files != nil {
		w.files[path] = true
	}
	active := true
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		key, written, args, err := splitLine(sc.Text())
		src := fmt.Sprintf("%s:%d", w.c.display(path), n)
		if err != nil {
			w.warn(fmt.Sprintf("%s: %v", src, err))
			continue
		}
		if key == "" {
			continue
		}
		if key == "include" {
			if !active {
				continue
			}
			for _, pat := range args {
				p := expandTilde(pat, w.c.Home)
				if !filepath.IsAbs(p) {
					p = filepath.Join(incDir, p)
				}
				matches, _ := filepath.Glob(filepath.Clean(p))
				sort.Strings(matches)
				for _, m := range matches {
					if st, err := os.Stat(m); err == nil && !st.IsDir() {
						w.read(m, incDir, depth+1)
					}
				}
			}
			continue
		}
		w.visit(&active, key, written, args, src)
	}
}

// all reads the user then the system file.
func (w *walker) all() {
	if w.c.UserFile != "" {
		w.read(w.c.UserFile, w.c.userIncludeDir(), 0)
	}
	if w.c.SystemFile != "" {
		w.read(w.c.SystemFile, filepath.Dir(w.c.SystemFile), 0)
	}
}

// Files lists the files that were looked for and those they include.
func (c *Config) Files() []File {
	var out []File
	seen := map[string]bool{}
	add := func(p, scope string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		f := File{Path: c.display(p), Scope: scope}
		if st, err := os.Stat(p); err == nil {
			f.Exists = !st.IsDir()
			if _, err := os.ReadFile(p); err != nil {
				f.Error = err.Error()
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			f.Error = err.Error()
		}
		out = append(out, f)
	}
	add(c.UserFile, "user")
	add(c.SystemFile, "system")
	w := &walker{c: c, warn: func(string) {}, files: map[string]bool{}, visit: func(*bool, string, string, []string, string) {}}
	w.all()
	var inc []string
	for p := range w.files {
		if !seen[p] {
			inc = append(inc, p)
		}
	}
	sort.Strings(inc)
	for _, p := range inc {
		add(p, "include")
	}
	return out
}

// Hosts lists the host names declared in the user's files (patterns with
// wildcards or negations are not hosts one can connect to).
func (c *Config) Hosts() []HostEntry {
	var out []HostEntry
	seen := map[string]bool{}
	w := &walker{c: c, warn: func(string) {}, visit: func(_ *bool, key, _ string, args []string, src string) {
		if key != "host" {
			return
		}
		for _, a := range args {
			if a == "" || strings.ContainsAny(a, "*?!") || seen[strings.ToLower(a)] {
				continue
			}
			seen[strings.ToLower(a)] = true
			out = append(out, HostEntry{Alias: a, Source: src})
		}
	}}
	if c.UserFile != "" {
		w.read(c.UserFile, c.userIncludeDir(), 0)
	}
	return out
}

// Resolve returns the configuration that applies to host (the name typed
// by the user, possibly an alias).
func (c *Config) Resolve(host string, q Query) *Host {
	h := &Host{Alias: host, cfg: c, query: q}
	e := &evaluator{c: c, h: h, hostArg: host}
	w := &walker{c: c, warn: func(s string) { h.warnf("%s", s) }, visit: e.line}
	w.all()
	if e.wantFinal {
		// like ssh: a second pass where Host lines see the final host name
		// and "Match final" applies
		e.final = true
		e.hostArg = h.HostName()
		w.all()
	}
	return h
}

type evaluator struct {
	c         *Config
	h         *Host
	hostArg   string
	final     bool
	wantFinal bool
}

func (e *evaluator) line(active *bool, key, written string, args []string, src string) {
	switch key {
	case "host":
		*active = matchList(e.hostArg, strings.Join(args, ","), true)
	case "match":
		*active = e.match(args, src)
	default:
		if *active {
			e.set(key, written, args, src)
		}
	}
}

func (e *evaluator) set(key, written string, args []string, src string) {
	name := canonical[key]
	if name == "" {
		name = written
	}
	val := strings.Join(args, " ")
	h := e.h
	switch {
	case multi[key]:
		for _, o := range h.Options {
			if strings.EqualFold(o.Key, name) && o.Value == val {
				return
			}
		}
	case key == "proxyjump" || key == "proxycommand":
		// whichever comes first prevents the other
		if h.Has("ProxyJump") || h.Has("ProxyCommand") {
			return
		}
	default:
		if h.Has(name) {
			return
		}
	}
	h.Options = append(h.Options, Option{Key: name, Value: val, Source: src})
}

// currentHost is what "Match host" compares: the HostName obtained so far.
func (e *evaluator) currentHost() string {
	if v := e.h.Value("HostName"); v != "" {
		return percentExpand(v, map[byte]string{'h': e.hostArg, '%': "%"})
	}
	return e.hostArg
}

func (e *evaluator) targetUser() string {
	if e.h.query.User != "" {
		return e.h.query.User
	}
	if v := e.h.Value("User"); v != "" {
		return v
	}
	return e.c.LocalUser
}

func (e *evaluator) match(args []string, src string) bool {
	if len(args) == 0 {
		e.h.warnf("%s: Match without criteria", src)
		return false
	}
	ok := true
	for i := 0; i < len(args); i++ {
		crit := strings.ToLower(args[i])
		neg := strings.HasPrefix(crit, "!")
		if neg {
			crit = crit[1:]
		}
		var r bool
		switch crit {
		case "all":
			r = true
		case "canonical", "final":
			e.wantFinal = true
			r = e.final
		case "host", "originalhost", "user", "localuser", "exec", "localnetwork", "tagged":
			if i+1 >= len(args) {
				e.h.warnf("%s: Match %s needs an argument", src, crit)
				return false
			}
			i++
			arg := args[i]
			switch crit {
			case "host":
				r = matchList(e.currentHost(), arg, true)
			case "originalhost":
				r = matchList(e.h.Alias, arg, true)
			case "user":
				r = matchList(e.targetUser(), arg, false)
			case "localuser":
				r = matchList(e.c.LocalUser, arg, false)
			case "tagged":
				r = matchList(e.h.Value("Tag"), arg, false)
			case "localnetwork":
				r = e.localNetwork(arg)
			case "exec":
				if !ok {
					continue // already false: do not run the command
				}
				r = e.exec(arg, src)
			}
		default:
			e.h.warnf("%s: unsupported Match criterion %q", src, crit)
			return false
		}
		if neg {
			r = !r
		}
		if !r {
			ok = false
		}
	}
	return ok
}

func (e *evaluator) localNetwork(list string) bool {
	if e.c.LocalAddrs == nil {
		return false
	}
	addrs := e.c.LocalAddrs()
	for _, cidr := range strings.Split(list, ",") {
		neg := strings.HasPrefix(cidr, "!")
		_, n, err := net.ParseCIDR(strings.TrimPrefix(strings.TrimSpace(cidr), "!"))
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n.Contains(a) {
				return !neg
			}
		}
	}
	return false
}

func (e *evaluator) exec(command, src string) bool {
	if e.c.Exec == nil {
		return false
	}
	if !e.h.shellSafe() {
		e.h.warnf("%s: Match exec not run: the host or user name contains shell characters", src)
		return false
	}
	return e.c.Exec(percentExpand(command, e.h.tokens(e.currentHost())))
}
