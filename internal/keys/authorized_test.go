package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func newPublic(t *testing.T) (string, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk, _ := ssh.NewPublicKey(pub)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk))), ssh.FingerprintSHA256(pk)
}

func TestParseAuthorized(t *testing.T) {
	a, fpA := newPublic(t)
	b, fpB := newPublic(t)
	ca, fpCA := newPublic(t)
	content := strings.Join([]string{
		"# clés de l'équipe",
		a + " alex@portable perso",
		"",
		`from="10.0.0.0/8,192.168.1.2",command="/usr/bin/rrsync -ro /srv",no-pty ` + b + " sauvegarde",
		"cert-authority,principals=\"alex\" " + ca + " CA de l'entreprise\r",
		"ssh-ed25519 pas-une-clé",
	}, "\n")
	keys := ParseAuthorized(content)
	if len(keys) != 4 {
		t.Fatalf("%d keys: %+v", len(keys), keys)
	}
	if k := keys[0]; k.Line != 2 || k.Fingerprint != fpA || k.Comment != "alex@portable perso" || k.Type != "ssh-ed25519" || len(k.Options) != 0 {
		t.Errorf("plain key: %+v", k)
	}
	if k := keys[1]; k.Line != 4 || k.Fingerprint != fpB || k.Comment != "sauvegarde" || len(k.Options) != 3 || k.Options[0] != `from="10.0.0.0/8,192.168.1.2"` {
		t.Errorf("key with options: %+v", k)
	}
	if k := keys[2]; !k.CA || k.Fingerprint != fpCA || k.Comment != "CA de l'entreprise" {
		t.Errorf("certificate authority: %+v", k)
	}
	if k := keys[3]; !k.Invalid || k.Line != 6 || k.Text != "ssh-ed25519 pas-une-clé" {
		t.Errorf("invalid line: %+v", k)
	}
	if version(content) == version(content+"\n") || len(version(content)) != 16 {
		t.Error("version")
	}
}

func TestFingerprintOfAKey(t *testing.T) {
	line, fp := newPublic(t)
	for _, in := range []string{fp, line, line + " un commentaire", "  " + fp + "\n"} {
		if got, err := Fingerprint(in); err != nil || got != fp {
			t.Errorf("Fingerprint(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := Fingerprint("pas une clé"); err == nil {
		t.Error("not a key")
	}
}
