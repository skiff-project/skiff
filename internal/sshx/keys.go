package sshx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kayrus/putty"
	"golang.org/x/crypto/ssh"

	"skiff/internal/prompt"
)

// LoadSigner loads a private key (OpenSSH, PEM, PKCS#8 or PuTTY .ppk),
// asking for the passphrase through pr when needed. If a matching
// "<path>-cert.pub" OpenSSH certificate exists it is attached.
func LoadSigner(ctx context.Context, path string, data []byte, passphrase string, pr prompt.Prompter) (ssh.Signer, error) {
	if data == nil {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}
	name := filepath.Base(path)
	if name == "." || name == "" {
		name = "private key"
	}
	parse := func(pass string) (ssh.Signer, error) {
		if bytes.HasPrefix(data, []byte("PuTTY-User-Key-File-")) {
			return parsePPK(data, pass)
		}
		if pass == "" {
			return ssh.ParsePrivateKey(data)
		}
		return ssh.ParsePrivateKeyWithPassphrase(data, []byte(pass))
	}
	s, err := parse(passphrase)
	if err == nil {
		return withCert(s, path), nil
	}
	if !isPassphraseErr(err) {
		return nil, err
	}
	if pr == nil {
		return nil, fmt.Errorf("key %s is encrypted and no passphrase was given", name)
	}
	for attempt := 0; attempt < 3; attempt++ {
		msg := "Enter the passphrase for key " + name
		if attempt > 0 {
			msg = "Wrong passphrase. " + msg
		}
		resp, perr := pr.Prompt(ctx, prompt.Request{
			Kind:    prompt.Passphrase,
			Title:   "Key passphrase",
			Message: msg,
			Fields:  []prompt.Field{{Label: "Passphrase", Secret: true}},
			Choices: []prompt.Choice{{ID: "ok", Label: "OK", Primary: true}, {ID: "cancel", Label: "Cancel"}},
		})
		if perr != nil {
			return nil, perr
		}
		if resp.Choice != "ok" || len(resp.Values) == 0 {
			return nil, prompt.ErrCancelled
		}
		s, err = parse(resp.Values[0])
		if err == nil {
			return withCert(s, path), nil
		}
		if !isPassphraseErr(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("key %s: wrong passphrase", name)
}

type ppkPassErr struct{ error }

func isPassphraseErr(err error) bool {
	var pm *ssh.PassphraseMissingError
	if errors.As(err, &pm) {
		return true
	}
	var pe ppkPassErr
	if errors.As(err, &pe) {
		return true
	}
	return errors.Is(err, x509.IncorrectPasswordError) || bytes.Contains([]byte(err.Error()), []byte("decryption password incorrect"))
}

func parsePPK(data []byte, pass string) (ssh.Signer, error) {
	k, err := putty.New(data)
	if err != nil {
		return nil, err
	}
	if k.Encryption != "none" && pass == "" {
		return nil, ppkPassErr{errors.New("ppk: passphrase required")}
	}
	raw, err := k.ParseRawPrivateKey([]byte(pass))
	if err != nil {
		if k.Encryption != "none" {
			return nil, ppkPassErr{err}
		}
		return nil, err
	}
	if p, ok := raw.(*ed25519.PrivateKey); ok {
		raw = *p
	}
	return ssh.NewSignerFromKey(raw)
}

func withCert(s ssh.Signer, path string) ssh.Signer {
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path + "-cert.pub")
	if err != nil {
		return s
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return s
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		return s
	}
	cs, err := ssh.NewCertSigner(cert, s)
	if err != nil {
		return s
	}
	return cs
}

// DefaultKeyFiles lists the usual OpenSSH identity files that exist.
func DefaultKeyFiles() []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_ed25519_sk", "id_ecdsa_sk"} {
		p := filepath.Join(h, ".ssh", n)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
