// Package sshx establishes SSH connections the way WinSCP does: every
// authentication method (public key incl. PuTTY .ppk, agent/Pageant,
// keyboard-interactive, password), interactive host key verification,
// jump-host chains, proxies, keepalives and algorithm selection.
package sshx

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"skiff/internal/netx"
	"skiff/internal/prompt"
)

// Target describes one SSH hop.
type Target struct {
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	User           string   `json:"user"`
	Password       string   `json:"-"`
	KeyFiles       []string `json:"keyFiles,omitempty"`
	KeyPEM         string   `json:"-"` // inline private key
	Passphrase     string   `json:"-"`
	UseAgent       bool     `json:"useAgent"`
	KbdInteractive bool     `json:"kbdInteractive"`
	// HostKey pins the expected host key fingerprint ("SHA256:..." or MD5 hex).
	HostKey string `json:"hostKey,omitempty"`

	// Settings that usually come from the OpenSSH configuration.
	AgentPath      string   `json:"-"` // agent socket or Windows pipe ("" = the default one)
	IdentitiesOnly bool     `json:"-"` // offer only the agent keys of KeyFiles
	CertFiles      []string `json:"-"` // certificates for the keys (besides <key>-cert.pub)
	NoPubkey       bool     `json:"-"`
	NoPassword     bool     `json:"-"`
	AuthOrder      []string `json:"-"` // PreferredAuthentications; methods not listed are not tried
	HostKeyAlias   string   `json:"-"` // name looked up in known_hosts instead of the host
	HostKeyPolicy  Policy   `json:"-"`
	// ForwardAgent gives the server access to the keychain and agent keys.
	ForwardAgent bool `json:"-"`
}

func (t Target) Addr() string {
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

// keyAddr is the name of the host in known_hosts.
func (t Target) keyAddr() string {
	if t.HostKeyAlias == "" {
		return t.Addr()
	}
	port := t.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(t.HostKeyAlias, strconv.Itoa(port))
}

// Options configures a connection.
type Options struct {
	Target    Target
	Jumps     []Target // dialled in order before Target
	Dial      netx.Options
	Keepalive time.Duration
	// KeepaliveCount is the number of unanswered keepalives before the
	// connection is closed (ServerAliveCountMax, default 3).
	KeepaliveCount int

	Ciphers      []string
	KeyExchanges []string
	MACs         []string
	HostKeyAlgos []string
	// Legacy enables insecure algorithms for old servers.
	Legacy bool

	// Keychain holds the keys of Skiff's keychain, offered like agent keys
	// (when the target uses the agent) and forwarded with ForwardAgent.
	Keychain []ssh.Signer

	HostKeys *HostKeyStore
	Prompter prompt.Prompter
	Banner   func(host, msg string)
	Logf     func(format string, args ...any)
	// OnPassword is called with the password that eventually succeeded so the
	// caller can cache it (used to reopen extra channels/connections).
	OnPassword func(host, password string)
}

// Info about an established connection, for the protocol info dialog.
type Info struct {
	ServerVersion     string
	ClientVersion     string
	HostKeyType       string
	FingerprintSHA256 string
	FingerprintMD5    string
	Kex               string
	CipherCS          string
	CipherSC          string
	MACCS             string
	MACSC             string
	AuthMethod        string
	RemoteAddr        string
	Jumps             int
}

// Conn wraps a client together with the jump clients it depends on.
type Conn struct {
	*ssh.Client
	Info  Info
	jumps []*ssh.Client
	stop  chan struct{}
	once  sync.Once
	// forward is set when the agent is forwarded to the server
	forward bool
	// Dead is closed when the connection is lost.
	Dead chan struct{}
}

func (c *Conn) Close() error {
	c.once.Do(func() { close(c.stop) })
	err := c.Client.Close()
	for i := len(c.jumps) - 1; i >= 0; i-- {
		c.jumps[i].Close()
	}
	return err
}

// DialerVia returns a netx.Dialer that opens TCP connections through this
// SSH connection (used for "tunnel" connections of other protocols).
func (c *Conn) DialerVia() netx.Dialer {
	return netx.DialFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
		type res struct {
			c   net.Conn
			err error
		}
		ch := make(chan res, 1)
		go func() {
			nc, err := c.Client.Dial("tcp", addr)
			ch <- res{nc, err}
		}()
		select {
		case r := <-ch:
			return r.c, r.err
		case <-ctx.Done():
			go func() {
				if r := <-ch; r.c != nil {
					r.c.Close()
				}
			}()
			return nil, ctx.Err()
		}
	})
}

// Dial connects to opts.Target, possibly through opts.Jumps.
func Dial(ctx context.Context, opts Options) (*Conn, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	base := netx.New(opts.Dial)
	var jumps []*ssh.Client
	cleanup := func() {
		for i := len(jumps) - 1; i >= 0; i-- {
			jumps[i].Close()
		}
	}
	hops := append(slices.Clone(opts.Jumps), opts.Target)
	var final *ssh.Client
	var info Info
	for i, hop := range hops {
		var nc net.Conn
		var err error
		addr := hop.Addr()
		if i == 0 {
			logf("Connecting to %s", addr)
			nc, err = base.DialContext(ctx, "tcp", addr)
		} else {
			logf("Connecting to %s through jump host %s", addr, hops[i-1].Addr())
			nc, err = (&Conn{Client: jumps[i-1]}).DialerVia().DialContext(ctx, "tcp", addr)
		}
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("connect %s: %w", addr, err)
		}
		cl, hi, err := handshake(ctx, nc, hop, &opts, logf)
		if err != nil {
			nc.Close()
			cleanup()
			return nil, err
		}
		if i < len(hops)-1 {
			jumps = append(jumps, cl)
		} else {
			final = cl
			info = hi
		}
	}
	info.Jumps = len(jumps)
	c := &Conn{Client: final, Info: info, jumps: jumps, stop: make(chan struct{}), Dead: make(chan struct{})}
	if opts.Target.ForwardAgent {
		fa := &forwardAgent{keys: opts.Keychain}
		if opts.Target.UseAgent {
			if sys, _, err := agentClient(opts.Target.AgentPath); err == nil {
				fa.sys = sys
			}
		}
		if err := agent.ForwardToAgent(final, fa); err != nil {
			logf("Agent forwarding unavailable: %v", err)
		} else {
			c.forward = true
			logf("SSH agent forwarded to the server")
		}
	}
	go func() {
		final.Wait()
		close(c.Dead)
	}()
	if opts.Keepalive > 0 {
		count := opts.KeepaliveCount
		if count <= 0 {
			count = 3
		}
		go c.keepalive(opts.Keepalive, count, logf)
	}
	return c, nil
}

func (c *Conn) keepalive(every time.Duration, count int, logf func(string, ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-c.Dead:
			return
		case <-t.C:
			done := make(chan error, 1)
			go func() {
				_, _, err := c.Client.SendRequest("keepalive@openssh.com", true, nil)
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					logf("Keepalive failed: %v", err)
					c.Client.Close()
					return
				}
			case <-time.After(every * time.Duration(count)):
				logf("Keepalive timed out, closing connection")
				c.Client.Close()
				return
			case <-c.stop:
				return
			}
		}
	}
}

func handshake(ctx context.Context, nc net.Conn, t Target, opts *Options, logf func(string, ...any)) (*ssh.Client, Info, error) {
	var info Info
	var mu sync.Mutex
	setAuth := func(m string) { mu.Lock(); info.AuthMethod = m; mu.Unlock() }

	cfg := &ssh.ClientConfig{
		User:          t.User,
		ClientVersion: "SSH-2.0-Skiff",
		Timeout:       0,
		BannerCallback: func(msg string) error {
			if opts.Banner != nil && strings.TrimSpace(msg) != "" {
				opts.Banner(t.Host, msg)
			}
			return nil
		},
	}
	cfg.Ciphers = opts.Ciphers
	cfg.KeyExchanges = opts.KeyExchanges
	cfg.MACs = opts.MACs
	cfg.HostKeyAlgorithms = opts.HostKeyAlgos
	if opts.Legacy {
		sup, ins := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
		if len(cfg.Ciphers) == 0 {
			cfg.Ciphers = append(sup.Ciphers, ins.Ciphers...)
		}
		if len(cfg.KeyExchanges) == 0 {
			cfg.KeyExchanges = append(sup.KeyExchanges, ins.KeyExchanges...)
		}
		if len(cfg.MACs) == 0 {
			cfg.MACs = append(sup.MACs, ins.MACs...)
		}
		if len(cfg.HostKeyAlgorithms) == 0 {
			cfg.HostKeyAlgorithms = append(sup.HostKeys, ins.HostKeys...)
		}
	}
	if opts.HostKeys != nil && len(cfg.HostKeyAlgorithms) == 0 {
		cfg.HostKeyAlgorithms = opts.HostKeys.PreferredAlgos(t.keyAddr(), t.HostKeyPolicy)
	}

	cfg.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		info.HostKeyType = key.Type()
		info.FingerprintSHA256 = ssh.FingerprintSHA256(key)
		info.FingerprintMD5 = md5Fingerprint(key)
		if t.HostKey != "" {
			if matchFingerprint(t.HostKey, key) {
				return nil
			}
			return fmt.Errorf("host key %s does not match the configured fingerprint %s", ssh.FingerprintSHA256(key), t.HostKey)
		}
		if opts.HostKeys == nil {
			return nil
		}
		return opts.HostKeys.CheckPolicy(ctx, t.keyAddr(), remote, key, opts.Prompter, t.HostKeyPolicy)
	}

	pr := opts.Prompter
	auth := map[string]ssh.AuthMethod{}

	signers, err := publicKeys(ctx, t, opts.Keychain, pr, logf)
	if err != nil {
		return nil, info, err
	}
	if len(signers) > 0 {
		auth["publickey"] = ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			setAuth("publickey")
			return signers, nil
		})
	}

	usedPassword := t.Password == ""
	var lastPassword string
	takePassword := func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		if !usedPassword {
			usedPassword = true
			return t.Password, true
		}
		return "", false
	}

	askPassword := func(title string) (string, error) {
		if pr == nil {
			return "", prompt.ErrNoUser
		}
		resp, err := pr.Prompt(ctx, prompt.Request{
			Kind:     prompt.Password,
			Title:    title,
			Message:  fmt.Sprintf("%s@%s", t.User, t.Host),
			Fields:   []prompt.Field{{Label: "Password", Secret: true}},
			Choices:  []prompt.Choice{{ID: "ok", Label: "OK", Primary: true}, {ID: "cancel", Label: "Cancel"}},
			Remember: true,
		})
		if err != nil {
			return "", err
		}
		if resp.Choice != "ok" || len(resp.Values) == 0 {
			return "", prompt.ErrCancelled
		}
		return resp.Values[0], nil
	}

	if t.KbdInteractive {
		auth["keyboard-interactive"] = ssh.RetryableAuthMethod(ssh.KeyboardInteractive(
			func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				setAuth("keyboard-interactive")
				if len(questions) == 0 {
					return nil, nil
				}
				if len(questions) == 1 && !echos[0] && strings.Contains(strings.ToLower(questions[0]), "password") {
					if pw, ok := takePassword(); ok {
						lastPassword = pw
						return []string{pw}, nil
					}
				}
				if pr == nil {
					return nil, prompt.ErrNoUser
				}
				fields := make([]prompt.Field, len(questions))
				for i, q := range questions {
					fields[i] = prompt.Field{Label: strings.TrimSpace(q), Secret: !echos[i]}
				}
				title := name
				if title == "" {
					title = "Authentication"
				}
				resp, err := pr.Prompt(ctx, prompt.Request{
					Kind:    prompt.KbdInteractive,
					Title:   title,
					Message: strings.TrimSpace(instruction),
					Fields:  fields,
					Choices: []prompt.Choice{{ID: "ok", Label: "OK", Primary: true}, {ID: "cancel", Label: "Cancel"}},
				})
				if err != nil {
					return nil, err
				}
				if resp.Choice != "ok" {
					return nil, prompt.ErrCancelled
				}
				ans := make([]string, len(questions))
				copy(ans, resp.Values)
				if len(questions) == 1 && !echos[0] {
					lastPassword = ans[0]
				}
				return ans, nil
			}), 3)
	}

	if !t.NoPassword {
		auth["password"] = ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
			setAuth("password")
			if pw, ok := takePassword(); ok {
				lastPassword = pw
				return pw, nil
			}
			pw, err := askPassword("Password")
			lastPassword = pw
			return pw, err
		}), 3)
	}
	order := t.AuthOrder
	if len(order) == 0 {
		order = []string{"publickey", "keyboard-interactive", "password"}
	}
	for _, m := range order {
		if a, ok := auth[m]; ok {
			cfg.Auth = append(cfg.Auth, a)
			delete(auth, m)
		}
	}

	// Cancel the handshake when ctx is cancelled.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			nc.Close()
		case <-done:
		}
	}()

	sc, chans, reqs, err := ssh.NewClientConn(nc, t.Addr(), cfg)
	if err != nil {
		if ctx.Err() != nil {
			return nil, info, ctx.Err()
		}
		return nil, info, fmt.Errorf("ssh %s: %w", t.Addr(), err)
	}
	info.ServerVersion = string(sc.ServerVersion())
	info.ClientVersion = string(sc.ClientVersion())
	info.RemoteAddr = sc.RemoteAddr().String()
	if am, ok := sc.(ssh.AlgorithmsConnMetadata); ok {
		a := am.Algorithms()
		info.Kex = a.KeyExchange
		info.CipherCS = a.Write.Cipher
		info.CipherSC = a.Read.Cipher
		info.MACCS = a.Write.MAC
		info.MACSC = a.Read.MAC
	}
	if lastPassword != "" && opts.OnPassword != nil {
		opts.OnPassword(t.Host, lastPassword)
	}
	logf("Authenticated as %s using %s", t.User, info.AuthMethod)
	return ssh.NewClient(sc, chans, reqs), info, nil
}

// publicKeys returns the keys to offer: the agent's first, then the key
// files whose key the agent does not hold (so that their passphrase is not
// asked for nothing), with their certificates.
func publicKeys(ctx context.Context, t Target, keychain []ssh.Signer, pr prompt.Prompter, logf func(string, ...any)) ([]ssh.Signer, error) {
	if t.NoPubkey {
		return nil, nil
	}
	var agentKeys []ssh.Signer
	nKeychain := 0
	if t.UseAgent {
		agentKeys = append(agentKeys, keychain...)
		if ag, err := agentSigners(t.AgentPath); err == nil {
			agentKeys = append(agentKeys, ag...)
		} else {
			logf("SSH agent unavailable: %v", err)
		}
	}
	inAgent := func(pub ssh.PublicKey) bool {
		for _, a := range agentKeys {
			if sameKey(a.PublicKey(), pub) {
				return true
			}
		}
		return false
	}
	var files []ssh.Signer
	var filePubs []ssh.PublicKey
	certFiles := append([]string(nil), t.CertFiles...)
	for _, kf := range t.KeyFiles {
		if kf == "" {
			continue
		}
		path := expandHome(kf)
		// as with ssh, the file may be the public key, the private key
		// being in the agent only (hardware token, password manager)
		pub := readPublicKey(path)
		pubOnly := pub != nil
		if pubOnly {
			path = strings.TrimSuffix(path, ".pub")
		} else {
			pub = readPublicKey(path + ".pub")
		}
		if pub != nil {
			filePubs = append(filePubs, pub)
			if inAgent(pub) {
				if _, err := os.Stat(path + "-cert.pub"); err == nil {
					certFiles = append(certFiles, path+"-cert.pub")
				}
				continue
			}
			if pubOnly {
				logf("Key %s is not in the SSH agent", kf)
				continue
			}
		}
		s, err := LoadSigner(ctx, path, nil, t.Passphrase, pr)
		if err != nil {
			if errors.Is(err, prompt.ErrCancelled) {
				return nil, err
			}
			logf("Cannot load key %s: %v", kf, err)
			continue
		}
		filePubs = append(filePubs, s.PublicKey())
		files = append(files, s)
	}
	if t.KeyPEM != "" {
		if s, err := LoadSigner(ctx, "", []byte(t.KeyPEM), t.Passphrase, pr); err == nil {
			filePubs = append(filePubs, s.PublicKey())
			files = append(files, s)
		} else {
			logf("Cannot load inline key: %v", err)
		}
	}
	if t.IdentitiesOnly {
		var kept []ssh.Signer
		for _, a := range agentKeys {
			for _, p := range filePubs {
				if sameKey(a.PublicKey(), p) {
					kept = append(kept, a)
					break
				}
			}
		}
		agentKeys = kept
	}
	for _, a := range agentKeys {
		for _, k := range keychain {
			if a == k {
				nKeychain++
			}
		}
	}
	if nKeychain > 0 {
		logf("Using %d key(s) from the Skiff keychain", nKeychain)
	}
	if n := len(agentKeys) - nKeychain; n > 0 {
		logf("Using %d key(s) from the SSH agent", n)
	}
	signers := append(agentKeys, files...)
	// explicit certificates go before the plain keys they certify
	var certs []ssh.Signer
	for _, cf := range certFiles {
		pub := readPublicKey(expandHome(cf))
		cert, ok := pub.(*ssh.Certificate)
		if !ok {
			logf("Cannot load certificate %s", cf)
			continue
		}
		for _, s := range signers {
			if sameKey(s.PublicKey(), cert.Key) {
				if cs, err := ssh.NewCertSigner(cert, s); err == nil {
					certs = append(certs, cs)
				}
			}
		}
	}
	return append(certs, signers...), nil
}

func readPublicKey(path string) ssh.PublicKey {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil
	}
	return pub
}

func sameKey(a, b ssh.PublicKey) bool {
	if c, ok := a.(*ssh.Certificate); ok {
		a = c.Key
	}
	if c, ok := b.(*ssh.Certificate); ok {
		b = c.Key
	}
	return string(a.Marshal()) == string(b.Marshal())
}

func md5Fingerprint(key ssh.PublicKey) string {
	sum := md5.Sum(key.Marshal())
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":")
}

func matchFingerprint(want string, key ssh.PublicKey) bool {
	w := strings.TrimSpace(want)
	// WinSCP's -hostkey accepts "ssh-ed25519 255 SHA256:xxx" style values
	if f := strings.Fields(w); len(f) > 1 {
		w = f[len(f)-1]
	}
	sha := ssh.FingerprintSHA256(key)
	if strings.EqualFold(w, sha) || w == strings.TrimPrefix(sha, "SHA256:") {
		return true
	}
	if strings.EqualFold(strings.TrimPrefix(w, "MD5:"), md5Fingerprint(key)) {
		return true
	}
	// base64 without padding variations
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.TrimPrefix(w, "SHA256:"), "=")); err == nil {
		return "SHA256:"+base64.RawStdEncoding.EncodeToString(b) == sha
	}
	return false
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
