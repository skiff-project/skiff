package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"skiff/internal/config"
	"skiff/internal/netx"
	"skiff/internal/sshconfig"
	"skiff/internal/sshx"
)

// The OpenSSH client configuration (ssh_config) completes what an SSH site
// leaves empty, like the ssh command does: a setting made in Skiff always
// wins over the file.

// Skiff's defaults for settings ssh_config may give instead.
const (
	defaultTimeout   = 15
	defaultKeepalive = 30
)

// SSHConfigOption is a value obtained from ssh_config and what Skiff does
// with it.
type SSHConfigOption struct {
	sshconfig.Option
	// Status is "applied", "site" (the site's own setting is used instead)
	// or "ignored" (a feature Skiff does not have).
	Status string `json:"status"`
}

// SSHConfigReport tells how the OpenSSH configuration applies to a host and
// what the connection will use.
type SSHConfigReport struct {
	Enabled  bool              `json:"enabled"`
	Alias    string            `json:"alias"`
	Options  []SSHConfigOption `json:"options"`
	Warnings []string          `json:"warnings,omitempty"`

	Host         string   `json:"host"`
	Port         int      `json:"port"`
	User         string   `json:"user"`
	KeyFiles     []string `json:"keyFiles,omitempty"`
	Agent        bool     `json:"agent"`
	Jumps        []string `json:"jumps,omitempty"`
	ProxyCommand string   `json:"proxyCommand,omitempty"`
	Forwards     []string `json:"forwards,omitempty"`
}

// SSHConfig returns the OpenSSH configuration in use, nil when disabled.
func (c *Core) SSHConfig() *sshconfig.Config {
	p := c.Store.Prefs().SSH
	if p.IgnoreConfig {
		return nil
	}
	cfg := sshconfig.Default()
	if p.ConfigFile != "" {
		cfg.UserFile = homePath(p.ConfigFile)
	}
	return cfg
}

func homePath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, filepath.FromSlash(strings.TrimLeft(p[1:], `/\`)))
		}
	}
	return p
}

// sshSetup is how Skiff reaches an SSH site.
type sshSetup struct {
	target         sshx.Target
	jumps          []sshx.Target
	proxy          *netx.ProxyConfig
	timeout        time.Duration
	keepalive      time.Duration
	keepaliveCount int
	ipVersion      string
	ciphers        []string
	kex            []string
	macs           []string
	hostKeyAlgos   []string
	forwards       []config.Forward
	report         SSHConfigReport
	applied        []string // "Key value" of the applied options, for the log
}

func seconds(n, def int) time.Duration {
	if n <= 0 {
		n = def
	}
	return time.Duration(n) * time.Second
}

// resolveSSH works out how to reach an SSH site: its own settings first,
// then the OpenSSH configuration of its host, then the defaults.
func (c *Core) resolveSSH(site config.Site, depth int) (*sshSetup, error) {
	if depth > 8 {
		return nil, errors.New("the jump hosts refer to each other")
	}
	s := &sshSetup{
		target: sshx.Target{
			Host: site.Host, Port: site.Port, User: site.User, Password: site.Password, Passphrase: site.Passphrase,
			UseAgent: site.UseAgent, KbdInteractive: site.KbdInteractive, HostKey: site.HostKey,
			ForwardAgent: site.ForwardAgent,
		},
		proxy:     proxyConfig(site),
		timeout:   seconds(site.Timeout, defaultTimeout),
		keepalive: time.Duration(site.Keepalive) * time.Second,
		ipVersion: site.IPVersion,
		ciphers:   site.Ciphers, kex: site.KeyExchanges, macs: site.MACs, hostKeyAlgos: site.HostKeyAlgos,
		forwards: site.Forwards,
	}
	if site.KeyFile != "" {
		if strings.Contains(site.KeyFile, "PRIVATE KEY") || strings.HasPrefix(site.KeyFile, "PuTTY-User-Key-File") {
			s.target.KeyPEM = site.KeyFile
		} else {
			s.target.KeyFiles = []string{site.KeyFile}
		}
	}
	jumps, err := c.jumpTargets(site, depth)
	if err != nil {
		return nil, err
	}
	s.jumps = jumps
	cfg := c.SSHConfig()
	s.report = SSHConfigReport{Enabled: cfg != nil && !site.IgnoreSSHConfig, Alias: site.Host, Options: []SSHConfigOption{}}
	configKeys := false
	if s.report.Enabled && site.Host != "" {
		h := cfg.Resolve(site.Host, sshconfig.Query{User: site.User, Port: site.Port})
		st := map[string]string{}
		explicitKeys := len(s.target.KeyFiles) > 0 || s.target.KeyPEM != ""
		configKeys = applyTarget(&s.target, h, explicitKeys, st)
		if err := c.applyConnection(s, site, cfg, h, st); err != nil {
			return nil, err
		}
		for _, o := range h.Options {
			status := st[strings.ToLower(o.Key)]
			if status == "" {
				status = "ignored"
			}
			s.report.Options = append(s.report.Options, SSHConfigOption{Option: o, Status: status})
			if status == "applied" {
				s.applied = append(s.applied, o.Key+" "+o.Value)
			}
		}
		s.report.Warnings = h.Warnings
	}
	finishTarget(&s.target, configKeys)
	r := &s.report
	r.Host, r.Port, r.User, r.KeyFiles = s.target.Host, s.target.Port, s.target.User, s.target.KeyFiles
	r.Agent = s.target.UseAgent && !s.target.NoPubkey
	for _, j := range s.jumps {
		r.Jumps = append(r.Jumps, fmt.Sprintf("%s@%s", j.User, j.Addr()))
	}
	if s.proxy != nil && s.proxy.Type == "command" {
		r.ProxyCommand = s.proxy.Command
	}
	for _, f := range s.forwards {
		r.Forwards = append(r.Forwards, f.Label)
	}
	return s, nil
}

// applyTarget completes the unset (zero) fields of t with the configuration
// of its host, recording in st what became of each keyword. It reports
// whether the configuration named the key files (the default ones are then
// not tried, as with ssh).
func applyTarget(t *sshx.Target, h *sshconfig.Host, explicitKeys bool, st map[string]string) bool {
	mark := func(key string, applied bool) {
		if applied {
			st[strings.ToLower(key)] = "applied"
		} else {
			st[strings.ToLower(key)] = "site"
		}
	}
	if h.Has("HostName") {
		t.Host = h.HostName()
		mark("HostName", true)
	}
	if h.Has("Port") {
		mark("Port", t.Port == 0)
		if t.Port == 0 {
			t.Port = h.Port()
		}
	}
	if h.Has("User") {
		mark("User", t.User == "")
		if t.User == "" {
			t.User = h.User()
		}
	}
	configKeys := false
	if h.Has("IdentityFile") {
		mark("IdentityFile", !explicitKeys)
		if !explicitKeys {
			configKeys = true
			for _, f := range h.IdentityFiles() {
				if _, err := os.Stat(f); err == nil {
					t.KeyFiles = append(t.KeyFiles, f)
				} else {
					h.Warnings = append(h.Warnings, fmt.Sprintf("IdentityFile %s: file not found", h.Display(f)))
				}
			}
		}
	}
	if v, set := h.Bool("IdentitiesOnly"); set {
		t.IdentitiesOnly = v
		mark("IdentitiesOnly", true)
	}
	if h.Has("CertificateFile") {
		t.CertFiles = h.CertificateFiles()
		mark("CertificateFile", true)
	}
	if h.Has("IdentityAgent") {
		mark("IdentityAgent", t.UseAgent)
		if t.UseAgent {
			path, off := h.IdentityAgent()
			t.UseAgent, t.AgentPath = !off, path
		}
	}
	if v, set := h.Bool("PubkeyAuthentication"); set {
		mark("PubkeyAuthentication", v || !explicitKeys)
		if !v && !explicitKeys {
			t.NoPubkey = true
		}
	}
	if v, set := h.Bool("PasswordAuthentication"); set {
		t.NoPassword = !v
		mark("PasswordAuthentication", true)
	}
	for _, k := range []string{"KbdInteractiveAuthentication", "ChallengeResponseAuthentication"} {
		if v, set := h.Bool(k); set {
			mark(k, t.KbdInteractive || !v)
			if !v {
				t.KbdInteractive = false
			}
			break
		}
	}
	if v := h.Value("PreferredAuthentications"); v != "" {
		for _, m := range strings.Split(v, ",") {
			t.AuthOrder = append(t.AuthOrder, strings.TrimSpace(m))
		}
		mark("PreferredAuthentications", true)
	}
	if v := h.Value("HostKeyAlias"); v != "" {
		t.HostKeyAlias = v
		mark("HostKeyAlias", true)
	}
	if v := h.Value("StrictHostKeyChecking"); v != "" {
		mark("StrictHostKeyChecking", t.HostKey == "")
		t.HostKeyPolicy.Strict = strings.ToLower(v)
	}
	if files, set := h.KnownHostsFiles(); set {
		t.HostKeyPolicy.Files, t.HostKeyPolicy.FilesSet = files, true
		for _, k := range []string{"UserKnownHostsFile", "GlobalKnownHostsFile"} {
			if h.Has(k) {
				mark(k, true)
			}
		}
	}
	if h.Has("Tag") {
		mark("Tag", true) // used by "Match tagged"
	}
	return configKeys
}

// finishTarget fills what neither the site nor ssh_config gave.
func finishTarget(t *sshx.Target, configKeys bool) {
	if t.Port == 0 {
		t.Port = 22
	}
	if t.User == "" {
		t.User = sshconfig.LocalUser()
	}
	if len(t.KeyFiles) == 0 && t.KeyPEM == "" && !configKeys {
		t.KeyFiles = sshx.DefaultKeyFiles()
	}
}

// applyConnection applies the settings of the connection itself: route
// (ProxyJump / ProxyCommand), timeouts, address family, algorithms and port
// forwarding.
func (c *Core) applyConnection(s *sshSetup, site config.Site, cfg *sshconfig.Config, h *sshconfig.Host, st map[string]string) error {
	mark := func(key string, applied bool) {
		if applied {
			st[strings.ToLower(key)] = "applied"
		} else {
			st[strings.ToLower(key)] = "site"
		}
	}
	ownRoute := len(site.Jumps) > 0 || s.proxy != nil
	if h.Has("ProxyJump") {
		mark("ProxyJump", !ownRoute)
		if !ownRoute {
			chain, proxy, err := configJumps(cfg, h.ProxyJump(), 0)
			if err != nil {
				return err
			}
			s.jumps, s.proxy = chain, proxy
		}
	}
	if h.Has("ProxyCommand") {
		mark("ProxyCommand", !ownRoute)
		if !ownRoute {
			proxy, err := proxyCommand(h)
			if err != nil {
				return err
			}
			s.proxy = proxy
		}
	}
	if h.Has("ProxyUseFdpass") {
		st["proxyusefdpass"] = "ignored"
	}
	if v, set := h.Bool("ForwardAgent"); set {
		mark("ForwardAgent", !site.ForwardAgent)
		if !site.ForwardAgent {
			s.target.ForwardAgent = v
		}
	}
	if n, ok := h.Int("ConnectTimeout"); ok && n > 0 {
		free := site.Timeout == 0 || site.Timeout == defaultTimeout
		mark("ConnectTimeout", free)
		if free {
			s.timeout = time.Duration(n) * time.Second
		}
	}
	if n, ok := h.Int("ServerAliveInterval"); ok && n >= 0 {
		free := site.Keepalive == defaultKeepalive
		mark("ServerAliveInterval", free)
		if free {
			s.keepalive = time.Duration(n) * time.Second
		}
	}
	if n, ok := h.Int("ServerAliveCountMax"); ok && n > 0 {
		s.keepaliveCount = n
		mark("ServerAliveCountMax", true)
	}
	if v := strings.ToLower(h.Value("AddressFamily")); v != "" {
		mark("AddressFamily", site.IPVersion == "")
		if site.IPVersion == "" {
			switch v {
			case "inet":
				s.ipVersion = "4"
			case "inet6":
				s.ipVersion = "6"
			}
		}
	}
	sup, ins := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
	for _, a := range []struct {
		key  string
		dest *[]string
		def  []string
		all  []string
	}{
		{"Ciphers", &s.ciphers, sup.Ciphers, append(append([]string{}, sup.Ciphers...), ins.Ciphers...)},
		{"KexAlgorithms", &s.kex, sup.KeyExchanges, append(append([]string{}, sup.KeyExchanges...), ins.KeyExchanges...)},
		{"MACs", &s.macs, sup.MACs, append(append([]string{}, sup.MACs...), ins.MACs...)},
		{"HostKeyAlgorithms", &s.hostKeyAlgos, sup.HostKeys, append(append([]string{}, sup.HostKeys...), ins.HostKeys...)},
	} {
		v := h.Value(a.key)
		if v == "" {
			continue
		}
		free := len(*a.dest) == 0 && !site.LegacyAlgos
		mark(a.key, free)
		if free {
			if list := sshconfig.Algorithms(v, a.def, a.all); len(list) > 0 {
				*a.dest = list
			} else {
				h.Warnings = append(h.Warnings, fmt.Sprintf("%s %s: no algorithm supported by Skiff, the defaults are used", a.key, v))
			}
		}
	}
	if fw := h.Forwards(); len(fw) > 0 {
		free := len(site.Forwards) == 0
		for _, k := range []string{"LocalForward", "RemoteForward", "DynamicForward"} {
			if h.Has(k) {
				mark(k, free)
			}
		}
		if free {
			names := map[string]string{"local": "LocalForward", "remote": "RemoteForward", "dynamic": "DynamicForward"}
			for _, f := range fw {
				s.forwards = append(s.forwards, config.Forward{
					ID: config.NewID(), Type: f.Type, BindHost: f.BindHost, BindPort: f.BindPort,
					DestHost: f.DestHost, DestPort: f.DestPort, AutoStart: true, Label: names[f.Type] + " " + f.Spec,
				})
			}
		}
	}
	return nil
}

func proxyCommand(h *sshconfig.Host) (*netx.ProxyConfig, error) {
	cmd, err := h.ProxyCommand()
	if err != nil {
		return nil, err
	}
	if cmd == "" { // ProxyCommand none
		return nil, nil
	}
	if v, _ := h.Bool("ProxyUseFdpass"); v {
		return nil, fmt.Errorf("ProxyCommand of %s: ProxyUseFdpass is not supported", h.Alias)
	}
	return &netx.ProxyConfig{Type: "command", Command: cmd, Expanded: true}, nil
}

// configJumps resolves ProxyJump hosts through the configuration. As with
// ssh, the first one may itself be reached through its own ProxyJump or
// ProxyCommand (returned as the proxy of the whole chain).
func configJumps(cfg *sshconfig.Config, hops []sshconfig.Hop, depth int) ([]sshx.Target, *netx.ProxyConfig, error) {
	if depth > 8 {
		return nil, nil, errors.New("ProxyJump: the jump hosts refer to each other")
	}
	var out []sshx.Target
	var proxy *netx.ProxyConfig
	for i, hop := range hops {
		t := sshx.Target{Host: hop.Host, Port: hop.Port, User: hop.User, UseAgent: true, KbdInteractive: true}
		h := cfg.Resolve(hop.Host, sshconfig.Query{User: hop.User, Port: hop.Port})
		configKeys := applyTarget(&t, h, false, map[string]string{})
		if i == 0 {
			if sub := h.ProxyJump(); len(sub) > 0 {
				chain, p, err := configJumps(cfg, sub, depth+1)
				if err != nil {
					return nil, nil, err
				}
				out, proxy = append(out, chain...), p
			} else if h.Has("ProxyCommand") {
				p, err := proxyCommand(h)
				if err != nil {
					return nil, nil, err
				}
				proxy = p
			}
		}
		finishTarget(&t, configKeys)
		out = append(out, t)
	}
	return out, proxy, nil
}

// SSHConfigReport tells how the OpenSSH configuration applies to a site
// (saved or being edited).
func (c *Core) SSHConfigReport(site config.Site) SSHConfigReport {
	site.ApplyDefaults()
	s, err := c.resolveSSH(site, 0)
	if err != nil {
		return SSHConfigReport{Enabled: c.SSHConfig() != nil && !site.IgnoreSSHConfig, Alias: site.Host, Options: []SSHConfigOption{}, Warnings: []string{err.Error()}}
	}
	return s.report
}
