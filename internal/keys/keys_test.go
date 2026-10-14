package keys

import (
	"bytes"
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
