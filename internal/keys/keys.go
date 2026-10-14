// Package keys generates and converts SSH key pairs (OpenSSH and PuTTY
// .ppk v3 formats) and installs public keys on servers — Skiff's built-in
// PuTTYgen / ssh-keygen / ssh-copy-id.
package keys

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/kayrus/putty"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"

	"skiff/internal/core"
	"skiff/internal/vfs"
)

type GenerateRequest struct {
	Type       string `json:"type"` // ed25519, rsa, ecdsa
	Bits       int    `json:"bits"`
	Comment    string `json:"comment"`
	Passphrase string `json:"passphrase"`
	Format     string `json:"format"` // openssh, ppk
	SaveTo     string `json:"saveTo,omitempty"`
}

type Generated struct {
	Private     string `json:"private"`
	Public      string `json:"public"`
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"`
	Path        string `json:"path,omitempty"`
	Format      string `json:"format"`
}

func Generate(r GenerateRequest) (Generated, error) {
	var key crypto.Signer
	var err error
	switch strings.ToLower(r.Type) {
	case "", "ed25519":
		_, key, err = ed25519.GenerateKey(rand.Reader)
	case "rsa":
		bits := r.Bits
		if bits == 0 {
			bits = 4096
		}
		if bits < 2048 {
			return Generated{}, errors.New("RSA keys must be at least 2048 bits")
		}
		key, err = rsa.GenerateKey(rand.Reader, bits)
	case "ecdsa":
		curve := elliptic.P256()
		switch r.Bits {
		case 384:
			curve = elliptic.P384()
		case 521:
			curve = elliptic.P521()
		}
		key, err = ecdsa.GenerateKey(curve, rand.Reader)
	default:
		return Generated{}, fmt.Errorf("unsupported key type %q", r.Type)
	}
	if err != nil {
		return Generated{}, err
	}
	return encode(key, r.Comment, r.Passphrase, r.Format, r.SaveTo)
}

func encode(key crypto.Signer, comment, passphrase, format, saveTo string) (Generated, error) {
	pub, err := ssh.NewPublicKey(key.Public())
	if err != nil {
		return Generated{}, err
	}
	g := Generated{
		Public:      strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + comment,
		Fingerprint: ssh.FingerprintSHA256(pub),
		Type:        pub.Type(),
		Format:      format,
	}
	g.Public = strings.TrimSpace(g.Public)
	switch format {
	case "ppk":
		g.Private, err = MarshalPPK(key, comment, passphrase)
	default:
		g.Format = "openssh"
		var block *pem.Block
		if passphrase != "" {
			block, err = ssh.MarshalPrivateKeyWithPassphrase(key, comment, []byte(passphrase))
		} else {
			block, err = ssh.MarshalPrivateKey(key, comment)
		}
		if err == nil {
			g.Private = string(pem.EncodeToMemory(block))
		}
	}
	if err != nil {
		return Generated{}, err
	}
	if saveTo != "" {
		saveTo = expand(saveTo)
		if _, err := os.Stat(saveTo); err == nil {
			return Generated{}, fmt.Errorf("%s already exists", saveTo)
		}
		if err := os.MkdirAll(filepath.Dir(saveTo), 0o700); err != nil {
			return Generated{}, err
		}
		if err := os.WriteFile(saveTo, []byte(g.Private), 0o600); err != nil {
			return Generated{}, err
		}
		pubPath := saveTo + ".pub"
		if strings.HasSuffix(saveTo, ".ppk") {
			pubPath = strings.TrimSuffix(saveTo, ".ppk") + ".pub"
		}
		os.WriteFile(pubPath, []byte(g.Public+"\n"), 0o644)
		g.Path = saveTo
	}
	return g, nil
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

// ---- PuTTY PPK v3 writer ----

func sshString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

func mpint(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) > 0 && b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return sshString(b)
}

func wrap64(s string) []string {
	var lines []string
	for len(s) > 64 {
		lines = append(lines, s[:64])
		s = s[64:]
	}
	if s != "" {
		lines = append(lines, s)
	}
	return lines
}

// MarshalPPK encodes a private key in PuTTY's PPK version 3 format.
func MarshalPPK(key crypto.Signer, comment, passphrase string) (string, error) {
	pub, err := ssh.NewPublicKey(key.Public())
	if err != nil {
		return "", err
	}
	algo := pub.Type()
	pubBlob := pub.Marshal()
	var priv []byte
	switch k := key.(type) {
	case ed25519.PrivateKey:
		priv = sshString(k.Seed())
	case *rsa.PrivateKey:
		k.Precompute()
		priv = append(priv, mpint(k.D)...)
		priv = append(priv, mpint(k.Primes[0])...)
		priv = append(priv, mpint(k.Primes[1])...)
		priv = append(priv, mpint(k.Precomputed.Qinv)...)
	case *ecdsa.PrivateKey:
		d, err := k.Bytes()
		if err != nil {
			return "", err
		}
		priv = mpint(new(big.Int).SetBytes(d))
	default:
		return "", fmt.Errorf("unsupported key type %T", key)
	}
	enc := "none"
	var cipherKey, iv, macKey []byte
	var kdf string
	if passphrase != "" {
		enc = "aes256-cbc"
		salt := make([]byte, 16)
		rand.Read(salt)
		const mem, passes, par = 8192, 21, 1
		out := argon2.IDKey([]byte(passphrase), salt, passes, mem, par, 80)
		cipherKey, iv, macKey = out[:32], out[32:48], out[48:]
		kdf = fmt.Sprintf("Key-Derivation: Argon2id\nArgon2-Memory: %d\nArgon2-Passes: %d\nArgon2-Parallelism: %d\nArgon2-Salt: %s\n", mem, passes, par, hex.EncodeToString(salt))
		// pad to the cipher block size with random bytes (PuTTY uses SHA-1 of data; any padding is accepted)
		if pad := len(priv) % aes.BlockSize; pad != 0 {
			p := make([]byte, aes.BlockSize-pad)
			rand.Read(p)
			priv = append(priv, p...)
		}
	}
	var macData []byte
	macData = append(macData, sshString([]byte(algo))...)
	macData = append(macData, sshString([]byte(enc))...)
	macData = append(macData, sshString([]byte(comment))...)
	macData = append(macData, sshString(pubBlob)...)
	macData = append(macData, sshString(priv)...)
	h := hmac.New(sha256.New, macKey)
	h.Write(macData)
	mac := hex.EncodeToString(h.Sum(nil))
	privOut := priv
	if enc != "none" {
		block, _ := aes.NewCipher(cipherKey)
		privOut = make([]byte, len(priv))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(privOut, priv)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "PuTTY-User-Key-File-3: %s\nEncryption: %s\nComment: %s\n", algo, enc, comment)
	pl := wrap64(base64.StdEncoding.EncodeToString(pubBlob))
	fmt.Fprintf(&sb, "Public-Lines: %d\n%s\n", len(pl), strings.Join(pl, "\n"))
	sb.WriteString(kdf)
	prl := wrap64(base64.StdEncoding.EncodeToString(privOut))
	fmt.Fprintf(&sb, "Private-Lines: %d\n%s\n", len(prl), strings.Join(prl, "\n"))
	fmt.Fprintf(&sb, "Private-MAC: %s\n", mac)
	return sb.String(), nil
}

// ---- conversion ----

type ConvertRequest struct {
	Path          string `json:"path,omitempty"`
	Data          string `json:"data,omitempty"`
	Passphrase    string `json:"passphrase"`
	NewPassphrase string `json:"newPassphrase"`
	Format        string `json:"format"` // openssh, ppk
	Comment       string `json:"comment"`
	SaveTo        string `json:"saveTo,omitempty"`
}

// Convert loads a private key in any supported format and re-encodes it.
func Convert(r ConvertRequest) (Generated, error) {
	data := []byte(r.Data)
	if r.Path != "" {
		var err error
		data, err = os.ReadFile(expand(r.Path))
		if err != nil {
			return Generated{}, err
		}
	}
	var raw any
	comment := r.Comment
	if bytes.HasPrefix(data, []byte("PuTTY-User-Key-File-")) {
		k, err := putty.New(data)
		if err != nil {
			return Generated{}, err
		}
		if comment == "" {
			comment = k.Comment
		}
		raw, err = k.ParseRawPrivateKey([]byte(r.Passphrase))
		if err != nil {
			return Generated{}, fmt.Errorf("cannot decrypt key: %w", err)
		}
		if p, ok := raw.(*ed25519.PrivateKey); ok {
			raw = *p
		}
	} else {
		var err error
		if r.Passphrase != "" {
			raw, err = ssh.ParseRawPrivateKeyWithPassphrase(data, []byte(r.Passphrase))
		} else {
			raw, err = ssh.ParseRawPrivateKey(data)
		}
		if err != nil {
			return Generated{}, err
		}
	}
	if p, ok := raw.(*ed25519.PrivateKey); ok {
		raw = *p
	}
	signer, ok := raw.(crypto.Signer)
	if !ok {
		return Generated{}, fmt.Errorf("unsupported key type %T", raw)
	}
	return encode(signer, comment, r.NewPassphrase, r.Format, r.SaveTo)
}

// ---- listing ----

type KeyFile struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
	Encrypted   bool   `json:"encrypted"`
	Format      string `json:"format"`
}

// List finds private keys in ~/.ssh.
func List() []KeyFile {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(h, ".ssh")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return []KeyFile{}
	}
	out := []KeyFile{}
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		st, err := e.Info()
		if err != nil || st.Size() > 64*1024 {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		kf := KeyFile{Path: p}
		switch {
		case bytes.HasPrefix(data, []byte("PuTTY-User-Key-File-")):
			k, err := putty.New(data)
			if err != nil {
				continue
			}
			kf.Format, kf.Type, kf.Comment, kf.Encrypted = "ppk", k.Algo, k.Comment, k.Encryption != "none"
			if pk, err := k.ParseRawPublicKey(); err == nil {
				if p, ok := pk.(*ed25519.PublicKey); ok {
					pk = *p
				}
				if sp, err := ssh.NewPublicKey(pk); err == nil {
					kf.Fingerprint = ssh.FingerprintSHA256(sp)
				}
			}
		case bytes.Contains(data, []byte("PRIVATE KEY")):
			kf.Format = "openssh"
			if pubData, err := os.ReadFile(p + ".pub"); err == nil {
				if pk, c, _, _, err := ssh.ParseAuthorizedKey(pubData); err == nil {
					kf.Type, kf.Fingerprint, kf.Comment = pk.Type(), ssh.FingerprintSHA256(pk), c
				}
			}
			if _, err := ssh.ParseRawPrivateKey(data); err != nil {
				var pm *ssh.PassphraseMissingError
				if errors.As(err, &pm) {
					kf.Encrypted = true
					if kf.Type == "" && pm.PublicKey != nil {
						kf.Type, kf.Fingerprint = pm.PublicKey.Type(), ssh.FingerprintSHA256(pm.PublicKey)
					}
				}
			} else if kf.Type == "" {
				if s, err := ssh.ParsePrivateKey(data); err == nil {
					kf.Type, kf.Fingerprint = s.PublicKey().Type(), ssh.FingerprintSHA256(s.PublicKey())
				}
			}
		default:
			continue
		}
		out = append(out, kf)
	}
	return out
}

// Install appends a public key to ~/.ssh/authorized_keys on the server
// (like ssh-copy-id), using only file operations so it also works on
// SFTP-only accounts.
func Install(ctx context.Context, c *core.Core, conn, publicKey string) (string, error) {
	publicKey = strings.TrimSpace(publicKey)
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey)); err != nil {
		return "", fmt.Errorf("invalid public key: %w", err)
	}
	f, err := c.FSFor(ctx, conn)
	if err != nil {
		return "", err
	}
	home, err := f.Home(ctx)
	if err != nil {
		return "", err
	}
	dir := vfs.Join("posix", home, ".ssh")
	ak := vfs.Join("posix", dir, "authorized_keys")
	if _, err := f.Stat(ctx, dir); err != nil {
		if err := f.Mkdir(ctx, dir); err != nil {
			return "", err
		}
		f.Chmod(ctx, dir, 0o700)
	}
	var existing []byte
	if r, err := f.Open(ctx, ak, 0); err == nil {
		existing, _ = io.ReadAll(io.LimitReader(r, 4<<20))
		r.Close()
	}
	fields := strings.Fields(publicKey)
	for _, ln := range strings.Split(string(existing), "\n") {
		lf := strings.Fields(ln)
		for i := 0; i+1 < len(lf); i++ {
			if len(fields) > 1 && lf[i] == fields[0] && lf[i+1] == fields[1] {
				return ak, nil // already installed
			}
		}
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += publicKey + "\n"
	w, err := f.Create(ctx, ak, vfs.CreateOpts{Size: int64(len(content))})
	if err != nil {
		return "", err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	f.Chmod(ctx, ak, 0o600)
	return ak, nil
}
