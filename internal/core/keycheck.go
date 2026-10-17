package core

import (
	"context"
	"errors"

	"skiff/internal/config"
	"skiff/internal/prompt"
	"skiff/internal/sshx"
)

// CheckKeyLogin opens a second SSH connection to the server of conn, made
// as the first one but offering only the key of this SHA256 fingerprint
// (from the Skiff keychain, the SSH agent or keyFile), and closes it: it
// tells whether the server lets that key in, before an older key is
// removed. The jump hosts authenticate as usual.
func (c *Core) CheckKeyLogin(ctx context.Context, conn, fingerprint, keyFile string, pr prompt.Prompter) error {
	cn, err := c.Conn(conn)
	if err != nil {
		return err
	}
	cn.mu.Lock()
	site := cn.site
	cn.mu.Unlock()
	if site.Protocol != config.ProtoSFTP && site.Protocol != config.ProtoSCP {
		return errors.New("only an SSH connection can check a key")
	}
	setup, err := c.resolveSSH(site, 0)
	if err != nil {
		return err
	}
	log := cn.Log()
	opts := c.sshOptions(setup, site, pr, log)
	t := &opts.Target
	t.KeyFiles, t.KeyPEM, t.CertFiles, t.Passphrase = nil, "", nil, ""
	if keyFile != "" {
		t.KeyFiles = []string{keyFile}
	}
	t.Password, t.KbdInteractive, t.NoPassword, t.NoPubkey = "", false, true, false
	t.UseAgent, t.IdentitiesOnly, t.ForwardAgent = true, false, false
	t.OnlyKey = fingerprint
	t.AuthOrder = []string{"publickey"}
	opts.Banner = nil
	opts.Keepalive = 0
	log.Logf("Checking that the server accepts the key %s", fingerprint)
	sc, err := sshx.Dial(ctx, opts)
	if err != nil {
		log.Warnf("The server did not accept the key %s: %v", fingerprint, err)
		return err
	}
	sc.Close()
	log.Logf("The server accepts the key %s", fingerprint)
	return nil
}
