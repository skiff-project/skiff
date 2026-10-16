package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"skiff/internal/prompt"
)

// HostKeyStore verifies host keys against Skiff's own known_hosts file and
// (read-only) the user's OpenSSH known_hosts, asking the user about unknown
// or changed keys.
type HostKeyStore struct {
	Path     string   // writable known_hosts maintained by Skiff
	ReadOnly []string // extra files consulted, e.g. ~/.ssh/known_hosts
	mu       sync.Mutex
	session  map[string]string // keys accepted "once" for this process: addr -> fingerprint
}

func NewHostKeyStore(path string) *HostKeyStore {
	s := &HostKeyStore{Path: path, session: map[string]string{}}
	h, _ := os.UserHomeDir()
	s.ReadOnly = OpenSSHKnownHosts(runtime.GOOS, h, os.Getenv)
	return s
}

// OpenSSHKnownHosts lists the known_hosts files ssh reads by default on an
// OS: the user's, then the system-wide ones (%ProgramData%\ssh on Windows).
func OpenSSHKnownHosts(goos, home string, getenv func(string) string) []string {
	if goos == "windows" {
		pd := getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return []string{home + `\.ssh\known_hosts`, home + `\.ssh\known_hosts2`, pd + `\ssh\ssh_known_hosts`, pd + `\ssh\ssh_known_hosts2`}
	}
	return []string{home + "/.ssh/known_hosts", home + "/.ssh/known_hosts2", "/etc/ssh/ssh_known_hosts", "/etc/ssh/ssh_known_hosts2"}
}

// Policy adapts the check to the OpenSSH configuration of a host.
type Policy struct {
	// Strict is StrictHostKeyChecking: "" or "ask" asks the user about an
	// unknown key, "yes" refuses it, "accept-new" (and "no") record it
	// silently. A changed key is never accepted silently.
	Strict string
	// Files, when set (UserKnownHostsFile / GlobalKnownHostsFile), are read
	// instead of the default OpenSSH known_hosts files.
	Files    []string
	FilesSet bool
}

func (s *HostKeyStore) files(pol Policy) []string {
	var out []string
	if _, err := os.Stat(s.Path); err == nil {
		out = append(out, s.Path)
	}
	extra := s.ReadOnly
	if pol.FilesSet {
		extra = pol.Files
	}
	for _, f := range extra {
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			out = append(out, f)
		}
	}
	return out
}

// callback reads the known_hosts files, leaving out a file that cannot be
// parsed rather than failing every connection.
func callback(files []string) (ssh.HostKeyCallback, error) {
	cb, err := knownhosts.New(files...)
	if err == nil || len(files) < 2 {
		return cb, err
	}
	var ok []string
	for _, f := range files {
		if _, err := knownhosts.New(f); err == nil {
			ok = append(ok, f)
		}
	}
	if len(ok) == 0 {
		return nil, err
	}
	return knownhosts.New(ok...)
}

// PreferredAlgos returns host key algorithms ordered so that a key type we
// already know for addr is negotiated first (avoids spurious "changed" warnings).
func (s *HostKeyStore) PreferredAlgos(addr string, pol Policy) []string {
	files := s.files(pol)
	if len(files) == 0 {
		return nil
	}
	cb, err := callback(files)
	if err != nil {
		return nil
	}
	// Ask the callback with a dummy key to learn which keys are known.
	var known []string
	dummy := dummyKey{}
	err = cb(addr, &net.TCPAddr{IP: net.IPv4zero}, dummy)
	var ke *knownhosts.KeyError
	if errors.As(err, &ke) {
		for _, k := range ke.Want {
			known = append(known, k.Key.Type())
		}
	}
	if len(known) == 0 {
		return nil
	}
	var out []string
	for _, t := range known {
		switch t {
		case ssh.KeyAlgoRSA:
			out = append(out, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			out = append(out, t)
		}
	}
	for _, a := range ssh.SupportedAlgorithms().HostKeys {
		if !contains(out, a) {
			out = append(out, a)
		}
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

// hostAddr is a host:port used as the remote address when the connection
// has none (local proxy command).
type hostAddr string

func (a hostAddr) Network() string { return "tcp" }
func (a hostAddr) String() string  { return string(a) }

type dummyKey struct{}

func (dummyKey) Type() string                        { return "skiff-dummy" }
func (dummyKey) Marshal() []byte                     { return []byte("skiff-dummy") }
func (dummyKey) Verify([]byte, *ssh.Signature) error { return errors.New("dummy") }

// Check verifies key for addr, prompting when unknown or changed.
func (s *HostKeyStore) Check(ctx context.Context, addr string, remote net.Addr, key ssh.PublicKey, pr prompt.Prompter) error {
	return s.CheckPolicy(ctx, addr, remote, key, pr, Policy{})
}

// CheckPolicy is Check following an OpenSSH host key policy.
func (s *HostKeyStore) CheckPolicy(ctx context.Context, addr string, remote net.Addr, key ssh.PublicKey, pr prompt.Prompter, pol Policy) error {
	fp := ssh.FingerprintSHA256(key)
	s.mu.Lock()
	if s.session[addr] == fp {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	files := s.files(pol)
	var ke *knownhosts.KeyError
	if _, _, err := net.SplitHostPort(remote.String()); err != nil {
		// not a TCP address (ProxyCommand): check the host name only
		remote = hostAddr(addr)
	}
	if len(files) > 0 {
		cb, err := callback(files)
		if err != nil {
			return fmt.Errorf("known_hosts: %w", err)
		}
		err = cb(addr, remote, key)
		if err == nil {
			return nil
		}
		if !errors.As(err, &ke) {
			var re *knownhosts.RevokedError
			if errors.As(err, &re) {
				return fmt.Errorf("host key for %s has been revoked", addr)
			}
			return err
		}
	}
	changed := ke != nil && len(ke.Want) > 0
	if !changed {
		switch strings.ToLower(pol.Strict) {
		case "yes", "true":
			return fmt.Errorf("host key for %s is unknown (%s %s) and StrictHostKeyChecking is yes", addr, key.Type(), fp)
		case "accept-new", "no", "off", "false":
			return s.Add(addr, key, false)
		}
	} else if strings.EqualFold(pol.Strict, "yes") || strings.EqualFold(pol.Strict, "accept-new") {
		return fmt.Errorf("WARNING: host key for %s has changed (now %s) and StrictHostKeyChecking is %s", addr, fp, pol.Strict)
	}
	if pr == nil {
		if changed {
			return fmt.Errorf("WARNING: host key for %s has changed (now %s)", addr, fp)
		}
		return fmt.Errorf("host key for %s is unknown (%s %s); accept it interactively or pin it with -hostkey", addr, key.Type(), fp)
	}
	host, _, _ := net.SplitHostPort(addr)
	details := map[string]string{
		"host":        addr,
		"keyType":     key.Type(),
		"sha256":      fp,
		"md5":         md5Fingerprint(key),
		"fingerprint": fp,
	}
	req := prompt.Request{
		Kind:    prompt.HostKey,
		Title:   "Unknown server",
		Message: fmt.Sprintf("The host key of %s is not cached. You have no guarantee that the server is the computer you think it is.", host),
		Details: details,
		Choices: []prompt.Choice{
			{ID: "accept", Label: "Accept", Primary: true},
			{ID: "once", Label: "Connect once"},
			{ID: "cancel", Label: "Cancel"},
		},
	}
	if changed {
		var old []string
		for _, w := range ke.Want {
			old = append(old, w.Key.Type()+" "+ssh.FingerprintSHA256(w.Key))
		}
		details["previous"] = strings.Join(old, "\n")
		req.Title = "Host key changed"
		req.Message = fmt.Sprintf("WARNING — POTENTIAL SECURITY BREACH! The host key of %s does not match the one Skiff has in cache. This could mean that an attacker is intercepting the connection, or that the server has been reinstalled.", host)
		req.Choices = []prompt.Choice{
			{ID: "accept", Label: "Update key", Danger: true},
			{ID: "once", Label: "Connect once"},
			{ID: "cancel", Label: "Cancel", Primary: true},
		}
		req.Danger = true
	}
	resp, err := pr.Prompt(ctx, req)
	if err != nil {
		return err
	}
	switch resp.Choice {
	case "accept":
		if err := s.Add(addr, key, changed); err != nil {
			return err
		}
		return nil
	case "once":
		s.mu.Lock()
		s.session[addr] = fp
		s.mu.Unlock()
		return nil
	}
	return prompt.ErrCancelled
}

// Add stores key for addr in Skiff's known_hosts, replacing older entries
// for the same host when replace is true.
func (s *HostKeyStore) Add(addr string, key ssh.PublicKey, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	norm := knownhosts.Normalize(addr)
	var lines []string
	if data, err := os.ReadFile(s.Path); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			if replace {
				f := strings.Fields(ln)
				if len(f) > 0 && contains(strings.Split(f[0], ","), norm) {
					continue
				}
			}
			lines = append(lines, ln)
		}
	}
	lines = append(lines, knownhosts.Line([]string{norm}, key))
	return os.WriteFile(s.Path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// Entry is a known host for the UI.
type KnownHost struct {
	Hosts       string `json:"hosts"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	Line        int    `json:"line"`
}

// List returns the entries of Skiff's own known_hosts.
func (s *HostKeyStore) List() ([]KnownHost, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []KnownHost
	for i, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		_, hosts, pub, _, _, err := ssh.ParseKnownHosts([]byte(ln))
		if err != nil {
			continue
		}
		out = append(out, KnownHost{Hosts: strings.Join(hosts, ","), KeyType: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), Line: i})
	}
	return out, nil
}

// Remove deletes the entry at the given line index.
func (s *HostKeyStore) Remove(line int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if line < 0 || line >= len(lines) {
		return errors.New("no such entry")
	}
	lines = append(lines[:line], lines[line+1:]...)
	return os.WriteFile(s.Path, []byte(strings.Join(lines, "\n")), 0o600)
}
