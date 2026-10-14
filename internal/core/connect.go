package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"skiff/internal/config"
	"skiff/internal/netx"
	"skiff/internal/prompt"
	"skiff/internal/sshx"
	"skiff/internal/vfs"
	"skiff/internal/vfs/davfs"
	"skiff/internal/vfs/ftpfs"
	"skiff/internal/vfs/s3fs"
	"skiff/internal/vfs/scpfs"
	"skiff/internal/vfs/sftpfs"
)

type dialed struct {
	fs     vfs.FS
	ssh    *sshx.Conn // SSH connection behind SFTP/SCP
	tunnel *sshx.Conn // jump chain used by FTP/WebDAV/S3
	tls    *netx.TLSVerifier
	// password typed interactively (to reuse for extra connections)
	password string
	remember bool
}

// rememberingPrompter records password prompts answered with "remember".
type rememberingPrompter struct {
	inner    prompt.Prompter
	password string
	remember bool
}

func (r *rememberingPrompter) Prompt(ctx context.Context, req prompt.Request) (prompt.Response, error) {
	resp, err := r.inner.Prompt(ctx, req)
	if err == nil && req.Kind == prompt.Password && len(resp.Values) > 0 {
		r.password = resp.Values[0]
		r.remember = resp.Remember
	}
	return resp, err
}

func (c *Core) jumpTargets(site config.Site) ([]sshx.Target, error) {
	var out []sshx.Target
	for _, j := range site.Jumps {
		if j.SiteID != "" {
			js, ok := c.Store.Site(j.SiteID)
			if !ok {
				return nil, fmt.Errorf("jump host site %q not found", j.SiteID)
			}
			r, err := c.Store.Resolve(js)
			if err != nil {
				return nil, err
			}
			// a jump site may itself have jumps
			sub, err := c.jumpTargets(r)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			out = append(out, siteTarget(r))
			continue
		}
		t := sshx.Target{Host: j.Host, Port: j.Port, User: j.User, Password: j.Password, UseAgent: true, KbdInteractive: true, HostKey: j.HostKey}
		if j.KeyFile != "" {
			t.KeyFiles = []string{j.KeyFile}
		} else {
			t.KeyFiles = sshx.DefaultKeyFiles()
		}
		out = append(out, t)
	}
	return out, nil
}

func siteTarget(s config.Site) sshx.Target {
	t := sshx.Target{
		Host: s.Host, Port: s.EffectivePort(), User: s.User, Password: s.Password,
		Passphrase: s.Passphrase, UseAgent: s.UseAgent, KbdInteractive: s.KbdInteractive, HostKey: s.HostKey,
	}
	if s.KeyFile != "" {
		if strings.Contains(s.KeyFile, "PRIVATE KEY") || strings.HasPrefix(s.KeyFile, "PuTTY-User-Key-File") {
			t.KeyPEM = s.KeyFile
		} else {
			t.KeyFiles = []string{s.KeyFile}
		}
	} else {
		t.KeyFiles = sshx.DefaultKeyFiles()
	}
	return t
}

func proxyConfig(s config.Site) *netx.ProxyConfig {
	p := s.Proxy
	if p.Type == "" || p.Type == "none" {
		return nil
	}
	return &netx.ProxyConfig{Type: p.Type, Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Command: p.Command, RemoteDNS: p.RemoteDNS}
}

func (c *Core) sshOptions(site config.Site, pr prompt.Prompter, log *Log, jumps []sshx.Target) sshx.Options {
	timeout := time.Duration(site.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return sshx.Options{
		Target:       siteTarget(site),
		Jumps:        jumps,
		Dial:         netx.Options{Timeout: timeout, IPVersion: site.IPVersion, Proxy: proxyConfig(site)},
		Keepalive:    time.Duration(site.Keepalive) * time.Second,
		Ciphers:      site.Ciphers,
		KeyExchanges: site.KeyExchanges,
		MACs:         site.MACs,
		HostKeyAlgos: site.HostKeyAlgos,
		Legacy:       site.LegacyAlgos,
		HostKeys:     c.HostKeys,
		Prompter:     pr,
		Logf:         log.Logf,
		Banner: func(host, msg string) {
			log.Logf("Banner from %s:\n%s", host, msg)
			c.Bus.Publish("banner", map[string]string{"host": host, "message": msg})
		},
	}
}

// askPassword prompts for a password before connecting (FTP/WebDAV/S3).
func askPassword(ctx context.Context, pr prompt.Prompter, site config.Site, label string) (string, error) {
	if pr == nil {
		return "", nil
	}
	resp, err := pr.Prompt(ctx, prompt.Request{
		Kind:     prompt.Password,
		Title:    label,
		Message:  fmt.Sprintf("%s@%s", site.User, site.Host),
		Fields:   []prompt.Field{{Label: label, Secret: true}},
		Choices:  []prompt.Choice{{ID: "ok", Label: "OK", Primary: true}, {ID: "cancel", Label: "Cancel"}},
		Remember: true,
	})
	if err != nil {
		return "", err
	}
	if resp.Choice != "ok" {
		return "", prompt.ErrCancelled
	}
	if len(resp.Values) == 0 {
		return "", nil
	}
	return resp.Values[0], nil
}

func (c *Core) dial(ctx context.Context, site config.Site, pr prompt.Prompter, log *Log) (*dialed, error) {
	rp := &rememberingPrompter{inner: pr}
	if pr == nil {
		rp = nil
	}
	var p prompt.Prompter
	if rp != nil {
		p = rp
	}
	jumps, err := c.jumpTargets(site)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(site.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	d := &dialed{}
	finish := func() *dialed {
		if rp != nil && rp.password != "" {
			d.password, d.remember = rp.password, rp.remember
		}
		return d
	}

	switch site.Protocol {
	case config.ProtoSFTP, config.ProtoSCP:
		opts := c.sshOptions(site, p, log, jumps)
		opts.OnPassword = func(host, pw string) {}
		conn, err := sshx.Dial(ctx, opts)
		if err != nil {
			return nil, err
		}
		d.ssh = conn
		if site.Protocol == config.ProtoSFTP {
			fs, err := sftpfs.New(conn, sftpfs.Options{ServerCommand: site.SftpServer, ResolveSymlinks: site.ResolveSymlinks, Logf: log.Logf})
			if err != nil {
				if strings.Contains(err.Error(), "subsystem request failed") || strings.Contains(err.Error(), "EOF") {
					log.Warnf("SFTP is not available (%v), falling back to SCP", err)
					d.fs = scpfs.New(conn, scpfs.Options{Shell: site.Shell, ListCommand: site.ListCommand, Logf: log.Logf})
					return finish(), nil
				}
				conn.Close()
				return nil, err
			}
			d.fs = fs
		} else {
			d.fs = scpfs.New(conn, scpfs.Options{Shell: site.Shell, ListCommand: site.ListCommand, Logf: log.Logf})
		}
		return finish(), nil
	}

	// Non-SSH protocols: optional SSH tunnel through the jump chain.
	var via netx.Dialer
	if len(jumps) > 0 {
		target := jumps[len(jumps)-1]
		topts := sshx.Options{
			Target: target, Jumps: jumps[:len(jumps)-1],
			Dial:     netx.Options{Timeout: timeout, IPVersion: site.IPVersion, Proxy: proxyConfig(site)},
			HostKeys: c.HostKeys, Prompter: p, Logf: log.Logf, Keepalive: 30 * time.Second,
		}
		tc, err := sshx.Dial(ctx, topts)
		if err != nil {
			return nil, fmt.Errorf("tunnel: %w", err)
		}
		d.tunnel = tc
		via = tc.DialerVia()
	}
	cleanup := func() {
		if d.tunnel != nil {
			d.tunnel.Close()
		}
	}
	nopts := netx.Options{Timeout: timeout, IPVersion: site.IPVersion, Via: via}
	if via == nil {
		nopts.Proxy = proxyConfig(site)
	}
	dialer := netx.New(nopts)
	tlsv := netx.NewTLSVerifier(site.Host, c.Store, p, site.TLSFingerprint, site.TLSInsecure)
	d.tls = tlsv

	password := site.Password
	switch site.Protocol {
	case config.ProtoFTP:
		if password == "" && site.User != "" && !strings.EqualFold(site.User, "anonymous") && !strings.EqualFold(site.User, "ftp") {
			pw, err := askPassword(ctx, p, site, "Password")
			if err != nil {
				cleanup()
				return nil, err
			}
			password = pw
		}
		cfg := ftpfs.Config{
			Host: site.Host, Port: site.EffectivePort(), User: site.User, Password: password, Account: site.FTPAccount,
			TLS: site.FTPS, Passive: !site.FTPActive, ActiveIP: site.FTPActiveIP, ForcePasvIP: site.FTPForcePasvIP,
			UseMLSD: site.FTPMLSD, ListHidden: site.FTPListHidden, Encoding: site.Encoding, Timeout: timeout,
			Dialer: dialer, TLSVerifier: tlsv, ViaProxy: via != nil || proxyConfig(site) != nil, Logf: log.Logf,
		}
		ka := time.Duration(site.Keepalive) * time.Second
		fs, err := ftpfs.New(ctx, cfg, ka)
		if err != nil {
			cleanup()
			return nil, err
		}
		d.fs = fs
	case config.ProtoWebDAV, config.ProtoS3:
		tr := &http.Transport{
			DialContext:           dialer.DialContext,
			TLSClientConfig:       tlsv.Config(site.Host),
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   timeout,
			ExpectContinueTimeout: time.Second,
		}
		if site.Protocol == config.ProtoWebDAV {
			if password == "" && site.User != "" {
				pw, err := askPassword(ctx, p, site, "Password")
				if err != nil {
					cleanup()
					return nil, err
				}
				password = pw
			}
			scheme := "http"
			if site.TLS {
				scheme = "https"
			}
			host := site.Host
			if site.Port != 0 && site.Port != config.DefaultPort(&site) {
				host = net.JoinHostPort(site.Host, strconv.Itoa(site.Port))
			}
			base := site.RemoteDir
			if base == "" {
				base = "/"
			}
			fs, err := davfs.New(ctx, davfs.Config{BaseURL: scheme + "://" + host + base, User: site.User, Password: password, Transport: tr, Timeout: timeout, Logf: log.Logf})
			if err != nil {
				cleanup()
				return nil, err
			}
			d.fs = fs
		} else {
			if password == "" && site.User != "" {
				pw, err := askPassword(ctx, p, site, "Secret access key")
				if err != nil {
					cleanup()
					return nil, err
				}
				password = pw
			}
			endpoint := site.Host
			if endpoint == "" {
				endpoint = "s3.amazonaws.com"
			}
			if site.Port != 0 && site.Port != config.DefaultPort(&site) {
				endpoint = net.JoinHostPort(endpoint, strconv.Itoa(site.Port))
			}
			fs, err := s3fs.New(ctx, s3fs.Config{
				Endpoint: endpoint, Region: site.S3Region, AccessKey: site.User, SecretKey: password,
				SessionToken: site.S3SessionToken, Secure: site.TLS, PathStyle: site.S3PathStyle,
				Transport: tr, Logf: log.Logf,
			})
			if err != nil {
				cleanup()
				return nil, err
			}
			d.fs = fs
		}
	default:
		cleanup()
		return nil, fmt.Errorf("unsupported protocol %q", site.Protocol)
	}
	if password != site.Password && rp != nil && rp.password == "" {
		rp.password = password
	}
	return finish(), nil
}

// friendlyError rewrites common low-level errors.
func friendlyError(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "unable to authenticate"):
		return errors.New("Authentication failed: the server rejected the credentials.")
	case strings.Contains(s, "connection refused"):
		return fmt.Errorf("Connection refused: %v", err)
	case strings.Contains(s, "no such host"):
		return fmt.Errorf("Host not found: %v", err)
	case strings.Contains(s, "i/o timeout") || strings.Contains(s, "deadline exceeded"):
		return fmt.Errorf("Connection timed out: %v", err)
	}
	return err
}
