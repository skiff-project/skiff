package sshconfig

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Host is the configuration that applies to one host.
type Host struct {
	Alias    string   `json:"alias"`   // the name given (an alias or a real host name)
	Options  []Option `json:"options"` // obtained values, in reading order
	Warnings []string `json:"warnings,omitempty"`
	cfg      *Config
	query    Query
}

// Display shortens a path under the home directory to ~.
func (h *Host) Display(p string) string { return h.cfg.display(p) }

func (h *Host) warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, w := range h.Warnings {
		if w == msg {
			return
		}
	}
	h.Warnings = append(h.Warnings, msg)
}

func (h *Host) find(key string) (Option, bool) {
	for _, o := range h.Options {
		if strings.EqualFold(o.Key, key) {
			return o, true
		}
	}
	return Option{}, false
}

// Has reports whether a value was obtained for key.
func (h *Host) Has(key string) bool {
	_, ok := h.find(key)
	return ok
}

// Value returns the first value obtained for key, "" when unset.
func (h *Host) Value(key string) string {
	o, _ := h.find(key)
	return o.Value
}

// Values returns every value obtained for a repeatable keyword.
func (h *Host) Values(key string) []string {
	var out []string
	for _, o := range h.Options {
		if strings.EqualFold(o.Key, key) {
			out = append(out, o.Value)
		}
	}
	return out
}

// Bool returns a yes/no value and whether it was set.
func (h *Host) Bool(key string) (val, set bool) {
	switch strings.ToLower(h.Value(key)) {
	case "yes", "true", "on":
		return true, true
	case "no", "false", "off":
		return false, true
	}
	return false, false
}

// Int returns a numeric value and whether it was set.
func (h *Host) Int(key string) (int, bool) {
	n, err := strconv.Atoi(h.Value(key))
	return n, err == nil
}

// HostName is the real host name to connect to (HostName, else the alias).
func (h *Host) HostName() string {
	if v := h.Value("HostName"); v != "" {
		return percentExpand(v, map[byte]string{'h': h.Alias, '%': "%"})
	}
	return h.Alias
}

// Port is the port asked for, else Port, else 22.
func (h *Host) Port() int {
	if h.query.Port > 0 {
		return h.query.Port
	}
	if p, ok := h.Int("Port"); ok && p > 0 && p < 65536 {
		return p
	}
	return 22
}

// User is the user asked for, else User, else the local user name.
func (h *Host) User() string {
	if h.query.User != "" {
		return h.query.User
	}
	if v := h.Value("User"); v != "" {
		return percentExpand(v, h.tokens(h.HostName()))
	}
	return h.cfg.LocalUser
}

// tokens are the %-tokens of ssh_config(5) (TOKENS section).
func (h *Host) tokens(remoteHost string) map[byte]string {
	c := h.cfg
	port := strconv.Itoa(h.Port())
	ruser := h.query.User
	if ruser == "" {
		ruser = h.Value("User")
	}
	if ruser == "" {
		ruser = c.LocalUser
	}
	jump := h.Value("ProxyJump")
	short := c.LocalHost
	if i := strings.IndexByte(short, '.'); i > 0 {
		short = short[:i]
	}
	sum := sha1.Sum([]byte(c.LocalHost + remoteHost + port + ruser + jump))
	alias := h.Value("HostKeyAlias")
	if alias == "" {
		alias = h.Alias
	}
	uid := ""
	if runtime.GOOS != "windows" {
		uid = strconv.Itoa(os.Getuid())
	}
	return map[byte]string{
		'%': "%", 'C': hex.EncodeToString(sum[:]), 'd': c.Home, 'h': remoteHost, 'i': uid, 'j': jump,
		'k': alias, 'L': short, 'l': c.LocalHost, 'n': h.Alias, 'p': port, 'r': ruser, 'u': c.LocalUser, 'T': "NONE",
	}
}

func percentExpand(s string, tok map[byte]string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+1 < len(s) {
			if v, ok := tok[s[i+1]]; ok {
				b.WriteString(v)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func expandTilde(p, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`):
		return filepath.Join(home, filepath.FromSlash(p[2:]))
	}
	return p
}

func (h *Host) expandEnv(s string) string {
	getenv := h.cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			return s
		}
		s = s[:i] + getenv(s[i+2:i+j]) + s[i+j+1:]
	}
}

// Path expands a file name like ssh does: ~, then ${VAR}, then %-tokens.
func (h *Host) Path(p string) string {
	p = expandTilde(p, h.cfg.Home)
	p = h.expandEnv(p)
	return percentExpand(p, h.tokens(h.HostName()))
}

// IdentityFiles returns the expanded IdentityFile values.
func (h *Host) IdentityFiles() []string {
	var out []string
	for _, v := range h.Values("IdentityFile") {
		if v != "" && !strings.EqualFold(v, "none") {
			out = append(out, h.Path(v))
		}
	}
	return out
}

// CertificateFiles returns the expanded CertificateFile values.
func (h *Host) CertificateFiles() []string {
	var out []string
	for _, v := range h.Values("CertificateFile") {
		if v != "" && !strings.EqualFold(v, "none") {
			out = append(out, h.Path(v))
		}
	}
	return out
}

// IdentityAgent returns the agent socket (or Windows pipe) to use: "" for
// the default one, and disabled for "none".
func (h *Host) IdentityAgent() (path string, disabled bool) {
	v := h.Value("IdentityAgent")
	switch {
	case v == "" || v == "SSH_AUTH_SOCK":
		return "", false
	case strings.EqualFold(v, "none"):
		return "", true
	case strings.HasPrefix(v, "$"):
		getenv := h.cfg.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		return getenv(v[1:]), false
	}
	return h.Path(v), false
}

// KnownHostsFiles returns the files given by UserKnownHostsFile and
// GlobalKnownHostsFile, and whether either was set (they then replace the
// default files).
func (h *Host) KnownHostsFiles() (files []string, set bool) {
	for _, k := range []string{"UserKnownHostsFile", "GlobalKnownHostsFile"} {
		v := h.Value(k)
		if v == "" {
			continue
		}
		set = true
		args, _ := splitArgs(v)
		for _, a := range args {
			if strings.EqualFold(a, "none") || a == "/dev/null" || strings.EqualFold(a, "NUL") {
				continue
			}
			files = append(files, h.Path(a))
		}
	}
	return files, set
}

// shellSafe reports whether the names substituted in commands (ProxyCommand,
// Match exec) are free of shell syntax, as ssh requires since 9.6.
func (h *Host) shellSafe() bool {
	for _, s := range []string{h.Alias, h.HostName(), h.query.User, h.Value("User")} {
		if strings.HasPrefix(s, "-") || strings.ContainsAny(s, "'`\"$\\;&<>|(){}*?!#~ \t\r\n") {
			return false
		}
	}
	return true
}

// ProxyCommand returns the expanded ProxyCommand ("" when unset or none).
func (h *Host) ProxyCommand() (string, error) {
	v := h.Value("ProxyCommand")
	if v == "" || strings.EqualFold(v, "none") {
		return "", nil
	}
	if !h.shellSafe() {
		return "", fmt.Errorf("ProxyCommand not run: the host or user name %q contains shell characters", h.Alias)
	}
	cmd := percentExpand(h.expandEnv(v), h.tokens(h.HostName()))
	if runtime.GOOS == "windows" {
		cmd = strings.TrimPrefix(cmd, "exec ")
	}
	return cmd, nil
}

// Hop is one host of a ProxyJump list.
type Hop struct {
	User string `json:"user,omitempty"`
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
}

func (j Hop) String() string {
	s := j.Host
	if strings.Contains(s, ":") {
		s = "[" + s + "]"
	}
	if j.User != "" {
		s = j.User + "@" + s
	}
	if j.Port != 0 {
		s += ":" + strconv.Itoa(j.Port)
	}
	return s
}

// ParseHop parses [ssh://][user@]host[:port] (IPv6 in brackets).
func ParseHop(s string) (Hop, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "ssh://"))
	var j Hop
	if i := strings.LastIndex(s, "@"); i >= 0 {
		j.User, s = s[:i], s[i+1:]
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return j, false
		}
		j.Host = s[1:end]
		if rest := s[end+1:]; strings.HasPrefix(rest, ":") {
			j.Port, _ = strconv.Atoi(rest[1:])
		}
	} else if i := strings.LastIndex(s, ":"); i > 0 && strings.Count(s, ":") == 1 {
		j.Host = s[:i]
		j.Port, _ = strconv.Atoi(s[i+1:])
	} else {
		j.Host = s
	}
	return j, j.Host != ""
}

// ProxyJump returns the jump hosts, in connection order.
func (h *Host) ProxyJump() []Hop {
	v := h.Value("ProxyJump")
	if v == "" || strings.EqualFold(v, "none") {
		return nil
	}
	var out []Hop
	for _, part := range strings.Split(v, ",") {
		if j, ok := ParseHop(part); ok {
			out = append(out, j)
		}
	}
	return out
}

// Forward is a LocalForward, RemoteForward or DynamicForward rule.
type Forward struct {
	Type     string `json:"type"` // local, remote, dynamic
	BindHost string `json:"bindHost,omitempty"`
	BindPort int    `json:"bindPort"`
	DestHost string `json:"destHost,omitempty"`
	DestPort int    `json:"destPort,omitempty"`
	Spec     string `json:"spec"`
}

// ParseForward parses "[bind_address:]port [host:hostport]".
func ParseForward(typ, spec string) (Forward, bool) {
	f := Forward{Type: typ, Spec: spec}
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return f, false
	}
	bind := fields[0]
	if i := strings.LastIndex(bind, ":"); i >= 0 {
		f.BindHost = strings.Trim(bind[:i], "[]")
		bind = bind[i+1:]
	}
	p, err := strconv.Atoi(bind)
	if err != nil {
		return f, false // Unix socket forwards are not supported
	}
	f.BindPort = p
	if typ != "dynamic" {
		if len(fields) < 2 {
			return f, false
		}
		d, ok := ParseHop(fields[1])
		if !ok || d.Port == 0 {
			return f, false
		}
		f.DestHost, f.DestPort = d.Host, d.Port
	}
	return f, true
}

// Forwards returns the forwarding rules that Skiff can start.
func (h *Host) Forwards() []Forward {
	var out []Forward
	for _, k := range []struct{ key, typ string }{{"LocalForward", "local"}, {"RemoteForward", "remote"}, {"DynamicForward", "dynamic"}} {
		for _, v := range h.Values(k.key) {
			if f, ok := ParseForward(k.typ, v); ok {
				out = append(out, f)
			}
		}
	}
	return out
}

// Algorithms applies an OpenSSH algorithm list (plain, or +append,
// -remove, ^prepend; wildcards allowed) to the default list, keeping only
// names Skiff supports.
func Algorithms(spec string, defaults, supported []string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	expand := func(list string) []string {
		var out []string
		for _, p := range strings.Split(list, ",") {
			p = strings.TrimSpace(p)
			for _, s := range supported {
				if wildcard(p, s) && !contains(out, s) {
					out = append(out, s)
				}
			}
		}
		return out
	}
	var out []string
	switch spec[0] {
	case '+':
		out = append(out, defaults...)
		for _, a := range expand(spec[1:]) {
			if !contains(out, a) {
				out = append(out, a)
			}
		}
	case '-':
		drop := expand(spec[1:])
		for _, a := range defaults {
			if !contains(drop, a) {
				out = append(out, a)
			}
		}
	case '^':
		out = expand(spec[1:])
		for _, a := range defaults {
			if !contains(out, a) {
				out = append(out, a)
			}
		}
	default:
		out = expand(spec)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
