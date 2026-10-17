//go:build integration

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/keys"
)

// testKeychain keeps a key in Skiff's keychain, logs in with it alone and
// forwards it to the server (ssh-add -l in a remote terminal).
func testKeychain(t *testing.T, e *e2e) {
	var g keys.Generated
	e.must(t, "keys.generate", keys.GenerateRequest{Type: "ed25519", Comment: "trousseau@e2e", Passphrase: "phrase-du-trousseau", Format: "openssh"}, &g)
	var k config.KeychainKey
	if err := e.mustFail(t, "keychain.add", map[string]string{"data": g.Private}); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("an encrypted key without its passphrase: %v", err)
	}
	e.must(t, "keychain.add", map[string]string{"data": g.Private, "passphrase": "phrase-du-trousseau"}, &k)
	if k.Fingerprint != g.Fingerprint || k.Name != "ssh-ed25519 "+strings.TrimPrefix(g.Fingerprint, "SHA256:")[:8] || k.Private != "" || !strings.HasPrefix(k.PublicKey, "ssh-ed25519 ") {
		t.Errorf("added %+v", k)
	}
	if err := e.mustFail(t, "keychain.add", map[string]string{"data": g.Private, "passphrase": "phrase-du-trousseau"}); err == nil {
		t.Error("the same key added twice")
	}
	// the private key is encrypted at rest
	if b, _ := os.ReadFile(filepath.Join(e.cfg, "keychain.json")); len(b) == 0 || strings.Contains(string(b), "PRIVATE KEY") {
		t.Errorf("keychain.json holds the key in clear: %.80s", b)
	}
	e.must(t, "keychain.rename", map[string]string{"id": k.ID, "name": "Clé du trousseau"}, nil)
	// generated straight into the keychain
	var g2 keys.Generated
	e.must(t, "keys.generate", keys.GenerateRequest{Type: "ecdsa", Bits: 256, Format: "ppk", Keychain: true, Name: "Générée"}, &g2)
	var list struct {
		Keys   []config.KeychainKey `json:"keys"`
		Locked bool                 `json:"locked"`
	}
	e.must(t, "keychain.list", nil, &list)
	if len(list.Keys) != 2 || list.Keys[0].Name != "Clé du trousseau" || list.Keys[1].Name != "Générée" || list.Keys[1].Fingerprint != g2.Fingerprint || list.Locked {
		t.Fatalf("list %+v", list)
	}
	ci := e.connect(t, "sftp")
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": g.Public}, nil)
	t.Cleanup(func() {
		e.call("keys.removeAuthorized", map[string]any{"conn": ci.ID, "fingerprints": []string{g.Fingerprint}, "force": true}, nil)
	})

	// only the keychain can log in: no password, no usable key file
	site := testSite("sftp")
	site.UseAgent, site.KbdInteractive, site.KeyFile = true, false, filepath.Join(e.home, "absent_key")
	logOf := func(id string) string {
		var lines []core.LogLine
		e.must(t, "conn.log", map[string]string{"conn": id}, &lines)
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(l.Msg + "\n")
		}
		return b.String()
	}
	t.Run("login", func(t *testing.T) {
		c := e.open(t, e.saveSite(t, site, "").ID)
		if l := logOf(c.ID); !strings.Contains(l, "key(s) from the Skiff keychain") || !strings.Contains(l, "using publickey") {
			t.Errorf("log:\n%s", l)
		}
	})
	t.Run("forwarding", func(t *testing.T) {
		site.ForwardAgent = true
		c := e.open(t, e.saveSite(t, site, "").ID)
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": c.ID, "cols": 120, "rows": 30}, &term)
		send, wait, done := e.termSession(t, term.ID)
		defer done()
		send("ssh-add -l; echo FIN-$((40+2))\n")
		out := wait("FIN-42")
		if !strings.Contains(out, g.Fingerprint) || !strings.Contains(out, g2.Fingerprint) {
			t.Errorf("forwarded agent does not list the keychain keys:\n%s", out)
		}
		// the server cannot change the agent
		send("ssh-add -D; echo FIN-$((40+3))\n")
		wait("FIN-43")
		send("ssh-add -l | wc -l; echo FIN-$((40+4))\n")
		if out := wait("FIN-44"); !strings.Contains(out, "2\r\n") {
			t.Errorf("keys removed from the server side:\n%s", out)
		}
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
		if l := logOf(c.ID); !strings.Contains(l, "SSH agent forwarded") {
			t.Errorf("log:\n%s", l)
		}
	})
	t.Run("forwardingThroughJump", func(t *testing.T) {
		// the bastion logs in with the keychain too; the agent goes to the
		// final server only, like ssh -A -J
		bastion := site
		bastion.ID, bastion.Name, bastion.ForwardAgent = "", "Bastion du trousseau", false
		b := e.saveSite(t, bastion, "")
		behind := site
		behind.ID, behind.Name, behind.Host, behind.ForwardAgent = "", "Derrière le bastion", "localhost", true // localhost: as seen from the bastion
		behind.Jumps = []config.Jump{{SiteID: b.ID}}
		c := e.open(t, e.saveSite(t, behind, "").ID)
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": c.ID, "cols": 120, "rows": 30}, &term)
		send, wait, done := e.termSession(t, term.ID)
		defer done()
		send("ssh-add -l; echo FIN-$((40+6))\n")
		if out := wait("FIN-46"); !strings.Contains(out, g.Fingerprint) || !strings.Contains(out, g2.Fingerprint) {
			t.Errorf("agent not forwarded through the jump host:\n%s", out)
		}
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
		if l := logOf(c.ID); !strings.Contains(l, "through jump host") || !strings.Contains(l, "SSH agent forwarded") {
			t.Errorf("log:\n%s", l)
		}
	})
	t.Run("noForwarding", func(t *testing.T) {
		site.ForwardAgent = false
		c := e.open(t, e.saveSite(t, site, "").ID)
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": c.ID, "cols": 120, "rows": 30}, &term)
		send, wait, done := e.termSession(t, term.ID)
		defer done()
		send("ssh-add -l; echo FIN-$((40+5))\n")
		if out := wait("FIN-45"); strings.Contains(out, g.Fingerprint) {
			t.Error("agent forwarded although turned off")
		}
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
	})
	t.Run("remove", func(t *testing.T) {
		for _, k := range list.Keys {
			e.must(t, "keychain.remove", map[string]string{"id": k.ID}, nil)
		}
		e.must(t, "keychain.list", nil, &list)
		if len(list.Keys) != 0 {
			t.Errorf("left %+v", list.Keys)
		}
		if err := e.mustFail(t, "conn.open", map[string]any{"site": site}); err == nil {
			t.Error("logged in without the removed keychain key")
		}
	})
}
