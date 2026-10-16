package keys

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayrus/putty"
	"golang.org/x/crypto/ssh"
)

func TestPPKRoundTrip(t *testing.T) {
	for _, typ := range []string{"ed25519", "ecdsa", "rsa"} {
		for _, pass := range []string{"", "correct horse"} {
			g, err := Generate(GenerateRequest{Type: typ, Bits: map[string]int{"rsa": 2048, "ecdsa": 256}[typ], Comment: "test", Passphrase: pass, Format: "ppk"})
			if err != nil {
				t.Fatalf("%s: %v", typ, err)
			}
			k, err := putty.New([]byte(g.Private))
			if err != nil {
				t.Fatalf("%s parse: %v", typ, err)
			}
			raw, err := k.ParseRawPrivateKey([]byte(pass))
			if err != nil {
				t.Fatalf("%s/%q decrypt: %v", typ, pass, err)
			}
			if p, ok := raw.(interface{ Public() any }); ok {
				_ = p
			}
			// convert back to OpenSSH and compare public keys
			conv, err := Convert(ConvertRequest{Data: g.Private, Passphrase: pass, Format: "openssh"})
			if err != nil {
				t.Fatalf("%s convert: %v", typ, err)
			}
			if conv.Fingerprint != g.Fingerprint {
				t.Errorf("%s: fingerprint changed %s -> %s", typ, g.Fingerprint, conv.Fingerprint)
			}
			s, err := ssh.ParsePrivateKey([]byte(conv.Private))
			if err != nil {
				t.Fatalf("%s openssh parse: %v", typ, err)
			}
			if !bytes.Equal(s.PublicKey().Marshal(), mustPub(t, g.Public).Marshal()) {
				t.Errorf("%s: public key mismatch", typ)
			}
		}
	}
}

func mustPub(t *testing.T, s string) ssh.PublicKey {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// List gives the public key of each key, without its passphrase.
func TestListPublicKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := home + "/.ssh"
	gen := func(name, pass, format string, keepPub bool) Generated {
		g, err := Generate(GenerateRequest{Type: "ed25519", Comment: "moi@pc", Passphrase: pass, Format: format, SaveTo: dir + "/" + name})
		if err != nil {
			t.Fatal(err)
		}
		if !keepPub {
			if err := os.Remove(dir + "/" + strings.TrimSuffix(name, ".ppk") + ".pub"); err != nil {
				t.Fatal(err)
			}
		}
		return g
	}
	want := map[string]struct {
		g       Generated
		comment bool
	}{
		"with_pub":       {gen("with_pub", "secret", "openssh", true), true},
		"encrypted_only": {gen("encrypted_only", "secret", "openssh", false), false},
		"plain_only":     {gen("plain_only", "", "openssh", false), false},
		"putty.ppk":      {gen("putty.ppk", "secret", "ppk", false), true},
	}
	list := List()
	if len(list) != len(want) {
		t.Fatalf("%d keys listed: %+v", len(list), list)
	}
	for _, k := range list {
		w, ok := want[filepath.Base(k.Path)]
		if !ok {
			t.Fatalf("unexpected %s", k.Path)
		}
		if k.PublicKey == "" {
			t.Errorf("%s: no public key", k.Path)
			continue
		}
		pk := mustPub(t, k.PublicKey)
		if !bytes.Equal(pk.Marshal(), mustPub(t, w.g.Public).Marshal()) {
			t.Errorf("%s: public key %q, want %q", k.Path, k.PublicKey, w.g.Public)
		}
		if w.comment != strings.HasSuffix(k.PublicKey, " moi@pc") {
			t.Errorf("%s: comment in %q", k.Path, k.PublicKey)
		}
		if k.Fingerprint != w.g.Fingerprint {
			t.Errorf("%s: fingerprint %s, want %s", k.Path, k.Fingerprint, w.g.Fingerprint)
		}
	}
}
