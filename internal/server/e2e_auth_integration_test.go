//go:build integration

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/keys"
	"skiff/internal/prompt"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
)

// startSSHAgent starts a real ssh-agent and returns its socket.
func startSSHAgent(t *testing.T) string {
	t.Helper()
	// short path: a Unix socket path is limited to about 100 bytes
	dir, err := os.MkdirTemp("", "skiff-agent")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "agent.sock")
	cmd := exec.Command("ssh-agent", "-D", "-a", sock)
	if err := cmd.Start(); err != nil {
		t.Fatalf("ssh-agent: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		os.RemoveAll(dir)
	})
	eventually(t, "ssh-agent", 10*time.Second, func() bool {
		_, err := os.Stat(sock)
		return err == nil
	})
	return sock
}

// sshAdd adds a key to the agent, giving its passphrase like a user would.
func sshAdd(t *testing.T, sock, key, passphrase string) {
	t.Helper()
	cmd := exec.Command("ssh-add", key)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	if passphrase != "" {
		askpass := filepath.Join(t.TempDir(), "askpass")
		os.WriteFile(askpass, []byte("#!/bin/sh\necho '"+passphrase+"'\n"), 0o700)
		cmd.Env = append(cmd.Env, "SSH_ASKPASS="+askpass, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=:0")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add %s: %v %s", key, err, out)
	}
}

// sshKeygen creates an OpenSSH key pair with ssh-keygen.
func sshKeygen(t *testing.T, path, passphrase string) string {
	t.Helper()
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-C", filepath.Base(path)+"@e2e", "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(path + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(pub))
}

// writeSSHConfig replaces ~/.ssh/config for the rest of the test.
func (e *e2e) writeSSHConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(e.home, ".ssh", "config")
	old, err := os.ReadFile(path)
	t.Cleanup(func() {
		if err == nil {
			os.WriteFile(path, old, 0o600)
		} else {
			os.Remove(path)
		}
	})
	os.WriteFile(path, []byte(content), 0o600)
}

// openAlias connects to a host of ~/.ssh/config by its alias, like the
// OpenSSH hosts of the interface.
func (e *e2e) openAlias(t *testing.T, alias string) (core.ConnInfo, error) {
	t.Helper()
	site := config.Site{Protocol: "sftp", Host: alias, Name: alias, UseAgent: true, KbdInteractive: true, ResolveSymlinks: true, Keepalive: 30, Timeout: 15}
	var c core.ConnInfo
	err := e.call("conn.open", map[string]any{"site": site}, &c)
	if err == nil {
		t.Cleanup(func() { e.call("conn.close", map[string]string{"conn": c.ID}, nil) })
	}
	return c, err
}

func (e *e2e) connLog(t *testing.T, id string) string {
	t.Helper()
	var lines []core.LogLine
	e.must(t, "conn.log", map[string]string{"conn": id}, &lines)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Msg + "\n")
	}
	return b.String()
}

// sshInfo returns the "SSH" group of the server information.
func (e *e2e) sshInfo(t *testing.T, id string) map[string]string {
	t.Helper()
	var info []vfs.InfoItem
	e.must(t, "conn.serverInfo", map[string]string{"conn": id}, &info)
	out := map[string]string{}
	for _, i := range info {
		if i.Group == "SSH" {
			out[i.Key] = i.Value
		}
	}
	return out
}

// keyOnlySite is an SSH site that can only log in with keys: no password,
// no keyboard-interactive (the password prompt is refused by the test).
func keyOnlySite(keyFile string, agent bool) config.Site {
	s := testSite("sftp")
	s.KeyFile, s.UseAgent, s.KbdInteractive = keyFile, agent, false
	return s
}

// testAgent logs in with the keys of a real ssh-agent: the agent alone,
// IdentitiesOnly, IdentityAgent (another socket, none) and a key file whose
// passphrase is not asked because the agent holds the key.
func testAgent(t *testing.T, e *e2e) {
	ssh := filepath.Join(e.home, ".ssh")
	sock := startSSHAgent(t)
	// a: installed, its private key only in the agent (hardware token,
	// password manager); b: not installed; enc: installed, encrypted file
	a := filepath.Join(ssh, "id_agent_a")
	b := filepath.Join(ssh, "id_agent_b")
	enc := filepath.Join(ssh, "id_agent_enc")
	pubA, pubEnc := sshKeygen(t, a, ""), sshKeygen(t, enc, "phrase-agent")
	sshKeygen(t, b, "")
	sshAdd(t, sock, a, "")
	sshAdd(t, sock, b, "")
	sshAdd(t, sock, enc, "phrase-agent")
	os.Remove(a)
	ci := e.connect(t, "sftp")
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": pubA}, nil)
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": pubEnc}, nil)
	t.Setenv("SSH_AUTH_SOCK", sock)

	t.Run("agentOnly", func(t *testing.T) {
		var info struct {
			Agent bool `json:"agent"`
		}
		e.must(t, "app.info", nil, &info)
		if !info.Agent {
			t.Error("app.info does not see the agent")
		}
		v := e.saveSite(t, keyOnlySite("", true), "")
		c := e.open(t, v.ID)
		log := e.connLog(t, c.ID)
		for _, want := range []string{"Using 3 key(s) from the SSH agent", "using publickey"} {
			if !strings.Contains(log, want) {
				t.Errorf("log lacks %q:\n%s", want, log)
			}
		}
		if got := e.sshInfo(t, c.ID)["Authentication"]; got != "publickey" {
			t.Errorf("authentication = %q", got)
		}
		// the same site without an agent has nothing to log in with
		t.Setenv("SSH_AUTH_SOCK", "")
		e.mustFail(t, "conn.open", map[string]any{"siteId": v.ID})
	})

	e.writeSSHConfig(t, `Host agent-all
    IdentitiesOnly no
    IdentityFile ~/.ssh/id_agent_b

Host agent-pub
    IdentityFile ~/.ssh/id_agent_a.pub

Host agent-nopriv
    IdentityFile ~/.ssh/id_agent_a

Host agent-other
    IdentityFile ~/.ssh/id_agent_b

Host agent-sock
    IdentitiesOnly no
    IdentityAgent `+sock+`

Host agent-env
    IdentitiesOnly no
    IdentityAgent $SKIFF_E2E_AGENT

Host agent-none
    IdentitiesOnly no
    IdentityAgent none

Host agent-*
    HostName 127.0.0.1
    Port 2222
    User skiff
    IdentitiesOnly yes
    PasswordAuthentication no
    KbdInteractiveAuthentication no
`)

	t.Run("identitiesOnly", func(t *testing.T) {
		// the agent offers only the key named by IdentityFile, given as the
		// public key or as the private key that is no longer on disk
		for _, alias := range []string{"agent-pub", "agent-nopriv"} {
			c, err := e.openAlias(t, alias)
			if err != nil {
				t.Errorf("%s: %v", alias, err)
				continue
			}
			if log := e.connLog(t, c.ID); !strings.Contains(log, "Using 1 key(s) from the SSH agent") || !strings.Contains(log, "using publickey") {
				t.Errorf("%s:\n%s", alias, log)
			}
		}
		// only b, which the server does not know, is offered
		if _, err := e.openAlias(t, "agent-other"); err == nil {
			t.Error("IdentitiesOnly did not filter the agent keys")
		}
		// without IdentitiesOnly, the other keys of the agent are offered too
		if c, err := e.openAlias(t, "agent-all"); err != nil {
			t.Errorf("agent-all: %v", err)
		} else if log := e.connLog(t, c.ID); !strings.Contains(log, "Using 3 key(s) from the SSH agent") {
			t.Errorf("agent-all:\n%s", log)
		}
	})

	t.Run("identityAgent", func(t *testing.T) {
		// no SSH_AUTH_SOCK: the socket comes from the configuration
		t.Setenv("SSH_AUTH_SOCK", "")
		t.Setenv("SKIFF_E2E_AGENT", sock)
		for _, alias := range []string{"agent-sock", "agent-env"} {
			if _, err := e.openAlias(t, alias); err != nil {
				t.Errorf("%s: %v", alias, err)
			}
		}
		if _, err := e.openAlias(t, "agent-all"); err == nil {
			t.Error("connected without any agent")
		}
		// "none" turns off the agent of SSH_AUTH_SOCK
		t.Setenv("SSH_AUTH_SOCK", sock)
		if _, err := e.openAlias(t, "agent-none"); err == nil {
			t.Error("IdentityAgent none: the agent was used")
		}
		var r core.SSHConfigReport
		e.must(t, "sshconfig.resolve", map[string]any{"site": config.Site{Protocol: "sftp", Host: "agent-none", UseAgent: true}}, &r)
		if r.Agent {
			t.Errorf("report of agent-none: %+v", r)
		}
	})

	t.Run("passphraseNotAsked", func(t *testing.T) {
		// an agent with this key only, so that the login is made with it
		only := startSSHAgent(t)
		sshAdd(t, only, enc, "phrase-agent")
		t.Setenv("SSH_AUTH_SOCK", only)
		asked := func() int { return len(e.promptsOf(prompt.Passphrase)) }
		n := asked()
		c := e.open(t, e.saveSite(t, keyOnlySite(enc, true), "").ID)
		if asked() != n {
			t.Error("passphrase asked although the agent holds the key")
		}
		if log := e.connLog(t, c.ID); !strings.Contains(log, "using publickey") {
			t.Errorf("log:\n%s", log)
		}
		// without the agent, the file needs its passphrase
		e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
			if ev.Kind == prompt.Passphrase {
				return prompt.Response{Choice: "ok", Values: []string{"phrase-agent"}}, true
			}
			return defaultPolicy(ev)
		})
		defer e.setPolicy(defaultPolicy)
		e.open(t, e.saveSite(t, keyOnlySite(enc, false), "").ID)
		if asked() != n+1 {
			t.Error("passphrase not asked without the agent")
		}
	})
}

// testCertificates logs in with OpenSSH user certificates signed by the
// certificate authority the SSH test server trusts (TrustedUserCAKeys, see
// testenv/ssh-init/60-user-ca). The keys are never in authorized_keys.
func testCertificates(t *testing.T, e *e2e) {
	ssh := filepath.Join(e.home, ".ssh")
	ci := e.connect(t, "sftp")
	ca := filepath.Join(t.TempDir(), "user_ca")
	os.WriteFile(ca, e.download(t, ci.ID, "/config/ssh_ca/user_ca"), 0o600)
	gen := func(t *testing.T, name, passphrase string) string {
		t.Helper()
		path := filepath.Join(ssh, name)
		e.must(t, "keys.generate", keys.GenerateRequest{Type: "ed25519", Comment: name, Passphrase: passphrase, Format: "openssh", SaveTo: path}, nil)
		return path
	}
	// sign writes <key>-cert.pub, as an administrator would with ssh-keygen
	sign := func(t *testing.T, key, principal string) string {
		t.Helper()
		if out, err := exec.Command("ssh-keygen", "-q", "-s", ca, "-I", filepath.Base(key)+"@e2e", "-n", principal, key+".pub").CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen -s: %v %s", err, out)
		}
		return key + "-cert.pub"
	}

	t.Run("nextToKey", func(t *testing.T) {
		key := gen(t, "id_cert", "")
		v := e.saveSite(t, keyOnlySite(key, false), "")
		e.mustFail(t, "conn.open", map[string]any{"siteId": v.ID}) // not signed yet
		sign(t, key, "skiff")
		c := e.open(t, v.ID)
		if log := e.connLog(t, c.ID); !strings.Contains(log, "using publickey") {
			t.Errorf("log:\n%s", log)
		}
		// a certificate for another user is refused
		other := gen(t, "id_cert_other", "")
		sign(t, other, "quelqu-un")
		e.mustFail(t, "conn.open", map[string]any{"siteId": e.saveSite(t, keyOnlySite(other, false), "").ID})
	})

	t.Run("certificateFile", func(t *testing.T) {
		key := gen(t, "id_certcfg", "")
		os.MkdirAll(filepath.Join(ssh, "certs"), 0o700)
		os.Rename(sign(t, key, "skiff"), filepath.Join(ssh, "certs", "certcfg-cert.pub"))
		e.writeSSHConfig(t, `Host cert-cfg
    CertificateFile ~/.ssh/certs/certcfg-cert.pub

Host cert-*
    HostName 127.0.0.1
    Port 2222
    User skiff
    IdentityFile ~/.ssh/id_certcfg
    PasswordAuthentication no
    KbdInteractiveAuthentication no
`)
		if _, err := e.openAlias(t, "cert-nocfg"); err == nil {
			t.Error("connected without the certificate")
		}
		if _, err := e.openAlias(t, "cert-cfg"); err != nil {
			t.Error(err)
		}
		var r core.SSHConfigReport
		e.must(t, "sshconfig.resolve", map[string]any{"site": config.Site{Protocol: "sftp", Host: "cert-cfg", UseAgent: true}}, &r)
		for _, o := range r.Options {
			if o.Key == "CertificateFile" && o.Status != "applied" {
				t.Errorf("CertificateFile %s", o.Status)
			}
		}
	})

	t.Run("agentKey", func(t *testing.T) {
		// the agent holds the key, the certificate sits next to the
		// (encrypted) key file: the passphrase is not asked
		sock := startSSHAgent(t)
		key := gen(t, "id_certagent", "phrase-cert")
		sshAdd(t, sock, key, "phrase-cert")
		sign(t, key, "skiff")
		t.Setenv("SSH_AUTH_SOCK", sock)
		n := len(e.promptsOf(prompt.Passphrase))
		e.open(t, e.saveSite(t, keyOnlySite(key, true), "").ID)
		if len(e.promptsOf(prompt.Passphrase)) != n {
			t.Error("passphrase asked although the agent holds the key")
		}
	})
}

// testLegacyAlgorithms connects to the old server of the test environment
// (SHA-1 key exchanges, ssh-rsa host key, CBC ciphers only), which needs
// the "allow legacy algorithms" site option or algorithms added in
// ssh_config.
func testLegacyAlgorithms(t *testing.T, e *e2e) {
	s := testSite("sftp")
	s.Port, s.Name = 2223, "ancien serveur"
	v := e.saveSite(t, s, "skiff")
	err := e.mustFail(t, "conn.open", map[string]any{"siteId": v.ID})
	if !strings.Contains(err.Error(), "no common algorithm") {
		t.Errorf("error without the option: %v", err)
	}
	s.ID, s.LegacyAlgos = v.ID, true
	c := e.open(t, e.saveSite(t, s, "").ID)
	info := e.sshInfo(t, c.ID)
	if !strings.HasPrefix(info["Key exchange"], "diffie-hellman-group1") || !strings.HasSuffix(info["Key exchange"], "-sha1") ||
		!strings.HasSuffix(info["Cipher (client → server)"], "-cbc") || info["MAC"] != "hmac-sha1" || info["Host key"] != "ssh-rsa" {
		t.Errorf("algorithms = %v", info)
	}
	root := e.remoteRoot(t, c)
	e.put(t, c.ID, root+"/ancien.txt", "ok\n")
	if got := e.readText(t, c.ID, root+"/ancien.txt"); got != "ok\n" {
		t.Errorf("read back %q", got)
	}

	t.Run("sshConfig", func(t *testing.T) {
		e.writeSSHConfig(t, `Host legacy-cfg
    HostName 127.0.0.1
    Port 2223
    User skiff
    KexAlgorithms diffie-hellman-group1-sha1
    HostKeyAlgorithms +ssh-rsa
    Ciphers +aes128-cbc
`)
		var c core.ConnInfo
		e.must(t, "conn.open", map[string]any{"site": config.Site{Protocol: "sftp", Host: "legacy-cfg", Timeout: 15}, "password": "skiff"}, &c)
		defer e.call("conn.close", map[string]string{"conn": c.ID}, nil)
		if info := e.sshInfo(t, c.ID); info["Key exchange"] != "diffie-hellman-group1-sha1" || info["Cipher (client → server)"] != "aes128-cbc" {
			t.Errorf("algorithms = %v", info)
		}
	})
}

// testImplicitFTPS connects to the implicit TLS FTPS server (vsftpd, the
// "port 990" style) by URL, lists and transfers both ways through the queue.
// testProtocols runs the whole file battery on it.
func testImplicitFTPS(t *testing.T, e *e2e) {
	var c core.ConnInfo
	e.must(t, "conn.open", map[string]any{"url": "ftps://skiff:skiff@127.0.0.1:9990/"}, &c)
	defer e.call("conn.close", map[string]string{"conn": c.ID}, nil)
	if c.Protocol != "ftp" || c.Port != 9990 {
		t.Errorf("conn = %+v", c)
	}
	if log := e.connLog(t, c.ID); strings.Contains(log, "AUTH TLS") {
		t.Errorf("explicit TLS negotiated on the implicit port:\n%s", log)
	}
	var info []vfs.InfoItem
	e.must(t, "conn.serverInfo", map[string]string{"conn": c.ID}, &info)
	tlsKeys := []string{}
	for _, i := range info {
		if i.Group == "TLS" {
			tlsKeys = append(tlsKeys, i.Key)
		}
	}
	if !slices.Contains(tlsKeys, "version") {
		t.Errorf("no TLS information: %+v", info)
	}
	root := e.remoteRoot(t, c)
	local := t.TempDir()
	os.WriteFile(filepath.Join(local, "implicite.txt"), []byte("TLS dès la connexion\n"), 0o644)
	j := e.enqueue(t, transfer.Request{Src: "local", Dst: c.ID, Paths: []string{filepath.Join(local, "implicite.txt")}, DstDir: root, Options: e.defaults(t, "local", c.ID)})
	if j.Status != transfer.Done {
		t.Fatalf("upload = %+v", j)
	}
	var lr core.ListResult
	e.must(t, "fs.list", map[string]string{"conn": c.ID, "path": root}, &lr)
	if len(lr.Entries) != 1 || lr.Entries[0].Name != "implicite.txt" || lr.Entries[0].Size != int64(len("TLS dès la connexion\n")) {
		t.Errorf("list = %+v", lr.Entries)
	}
	back := filepath.Join(local, "retour")
	j = e.enqueue(t, transfer.Request{Src: c.ID, Dst: "local", Paths: []string{root + "/implicite.txt"}, DstDir: back, Options: e.defaults(t, c.ID, "local")})
	if b, _ := os.ReadFile(filepath.Join(back, "implicite.txt")); j.Status != transfer.Done || string(b) != "TLS dès la connexion\n" {
		t.Errorf("download = %+v %q", j, b)
	}
}
