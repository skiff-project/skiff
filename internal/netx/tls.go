package netx

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"skiff/internal/prompt"
)

// CertStore remembers TLS certificates the user chose to trust.
type CertStore interface {
	IsTrusted(host, fingerprint string) bool
	Trust(host, fingerprint string) error
}

// Fingerprint returns the colon separated SHA-256 fingerprint of a cert.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return colonHex(sum[:])
}

func colonHex(b []byte) string {
	h := hex.EncodeToString(b)
	var sb strings.Builder
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(h[i : i+2])
	}
	return strings.ToUpper(sb.String())
}

// TLSVerifier builds tls.Configs that verify certificates normally and fall
// back to asking the user (once per session) when verification fails.
type TLSVerifier struct {
	Host     string
	Store    CertStore
	Prompter prompt.Prompter
	// Pinned SHA-256 fingerprint configured on the site (optional).
	Pinned string
	// Insecure disables verification completely (explicit user setting).
	Insecure bool

	mu       sync.Mutex
	accepted map[string]bool
	Cache    tls.ClientSessionCache
	// Last holds the peer certificate chain of the last handshake (for the info dialog).
	Last []*x509.Certificate
	// LastState holds the connection state of the last handshake.
	LastState *tls.ConnectionState
}

func NewTLSVerifier(host string, store CertStore, p prompt.Prompter, pinned string, insecure bool) *TLSVerifier {
	return &TLSVerifier{
		Host: host, Store: store, Prompter: p, Pinned: pinned, Insecure: insecure,
		accepted: map[string]bool{},
		Cache:    tls.NewLRUClientSessionCache(64),
	}
}

// Config returns a tls.Config for serverName.
func (v *TLSVerifier) Config(serverName string) *tls.Config {
	return &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, // verification is done in VerifyConnection
		ClientSessionCache: v.Cache,
		MinVersion:         tls.VersionTLS10,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return v.verify(cs)
		},
	}
}

func (v *TLSVerifier) verify(cs tls.ConnectionState) error {
	v.mu.Lock()
	v.Last = cs.PeerCertificates
	st := cs
	v.LastState = &st
	v.mu.Unlock()
	if len(cs.PeerCertificates) == 0 {
		return errors.New("tls: server sent no certificate")
	}
	leaf := cs.PeerCertificates[0]
	fp := Fingerprint(leaf)
	if v.Insecure {
		return nil
	}
	if v.Pinned != "" {
		if normalizeFP(v.Pinned) == normalizeFP(fp) {
			return nil
		}
		return fmt.Errorf("tls: certificate fingerprint %s does not match the configured one", fp)
	}
	v.mu.Lock()
	ok := v.accepted[fp]
	v.mu.Unlock()
	if ok {
		return nil
	}
	inter := x509.NewCertPool()
	for _, c := range cs.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	_, verr := leaf.Verify(x509.VerifyOptions{DNSName: cs.ServerName, Intermediates: inter})
	if verr == nil {
		return nil
	}
	if v.Store != nil && v.Store.IsTrusted(v.Host, fp) {
		v.remember(fp)
		return nil
	}
	if v.Prompter == nil {
		return verr
	}
	sha1sum := sha1.Sum(leaf.Raw)
	resp, err := v.Prompter.Prompt(context.Background(), prompt.Request{
		Kind:    prompt.Certificate,
		Title:   "Untrusted certificate",
		Message: "The server's certificate could not be verified: " + verr.Error(),
		Details: map[string]string{
			"host":        v.Host,
			"subject":     leaf.Subject.String(),
			"issuer":      leaf.Issuer.String(),
			"validFrom":   leaf.NotBefore.Format("2006-01-02 15:04"),
			"validTo":     leaf.NotAfter.Format("2006-01-02 15:04"),
			"sha256":      fp,
			"sha1":        colonHex(sha1sum[:]),
			"names":       strings.Join(leaf.DNSNames, ", "),
			"error":       verr.Error(),
			"fingerprint": fp,
		},
		Choices: []prompt.Choice{
			{ID: "always", Label: "Trust always", Primary: true},
			{ID: "once", Label: "Trust this time"},
			{ID: "cancel", Label: "Cancel"},
		},
		Danger: true,
	})
	if err != nil {
		return err
	}
	switch resp.Choice {
	case "always":
		if v.Store != nil {
			v.Store.Trust(v.Host, fp)
		}
		v.remember(fp)
		return nil
	case "once":
		v.remember(fp)
		return nil
	}
	return prompt.ErrCancelled
}

func (v *TLSVerifier) remember(fp string) {
	v.mu.Lock()
	v.accepted[fp] = true
	v.mu.Unlock()
}

func normalizeFP(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "SHA256:")
	return strings.NewReplacer(":", "", " ", "", "-", "").Replace(s)
}

// TLSInfo describes the negotiated TLS parameters for the info dialog.
func TLSInfo(cs *tls.ConnectionState) map[string]string {
	if cs == nil {
		return nil
	}
	out := map[string]string{
		"version": tls.VersionName(cs.Version),
		"cipher":  tls.CipherSuiteName(cs.CipherSuite),
	}
	if len(cs.PeerCertificates) > 0 {
		c := cs.PeerCertificates[0]
		out["subject"] = c.Subject.String()
		out["issuer"] = c.Issuer.String()
		out["expires"] = c.NotAfter.Format("2006-01-02")
		out["fingerprint"] = Fingerprint(c)
	}
	return out
}
