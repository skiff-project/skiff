package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"skiff/frontend"
	"skiff/internal/core"
	"skiff/internal/server"
)

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:8443", "address to listen on (use 0.0.0.0:8443 for remote access)")
	user := fs.String("user", "", "login name (default: saved one, or \"admin\")")
	setPassword := fs.Bool("set-password", false, "set (or change) the web login password and exit")
	passwordEnv := fs.String("password-env", "", "read the login password from this environment variable")
	certFile := fs.String("tls-cert", "", "TLS certificate (PEM)")
	keyFile := fs.String("tls-key", "", "TLS private key (PEM)")
	noTLS := fs.Bool("no-tls", false, "serve plain HTTP (only behind a TLS reverse proxy!)")
	root := fs.String("root", "", "restrict the local file panel to this directory")
	localTerm := fs.Bool("allow-local-terminal", false, "allow terminals on the Skiff host")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: skiff serve [flags]\n\nServes the Skiff UI over HTTPS so it can be used from any browser\n(server, NAS, headless machine). A self-signed certificate is generated\nwhen none is given.\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	store, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}
	prefs := store.Prefs()
	name := *user
	if name == "" {
		name = prefs.Web.User
	}
	if name == "" {
		name = "admin"
	}
	hash := prefs.Web.PasswordHash
	if *passwordEnv != "" {
		pw := os.Getenv(*passwordEnv)
		if pw == "" {
			fmt.Fprintf(os.Stderr, "environment variable %s is empty\n", *passwordEnv)
			return 1
		}
		hash = server.HashPassword(pw)
	}
	if *setPassword || hash == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) && *passwordEnv == "" {
			fmt.Fprintln(os.Stderr, "no web password configured: run \"skiff serve --set-password\" in a terminal or use --password-env")
			return 1
		}
		if *passwordEnv == "" {
			fmt.Printf("Choose the password for web user %q: ", name)
			p1, _ := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Print("\nRepeat: ")
			p2, _ := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if string(p1) != string(p2) || len(p1) < 8 {
				fmt.Fprintln(os.Stderr, "passwords differ or are shorter than 8 characters")
				return 1
			}
			hash = server.HashPassword(string(p1))
		}
		prefs.Web.User, prefs.Web.PasswordHash = name, hash
		if err := store.SetPrefs(prefs); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Web credentials saved.")
		if *setPassword {
			return 0
		}
	}

	if *root != "" {
		abs, err := filepath.Abs(*root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		*root = abs
	}
	c := core.New(store, core.Options{LocalRoot: *root})
	defer c.Shutdown()
	srv := server.New(c, server.Options{
		Mode: server.Web, Version: version, User: name, PasswordHash: hash, Licences: licences(),
		Static: frontend.Dist(), AllowLocalTerminal: *localTerm, Logf: log.Printf,
	})

	cert, key := *certFile, *keyFile
	if !*noTLS && cert == "" {
		cert, key, err = selfSigned(store.Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "certificate:", err)
			return 1
		}
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	scheme := "https"
	if *noTLS {
		scheme = "http"
		if host, _, _ := net.SplitHostPort(*listen); host != "127.0.0.1" && host != "localhost" && host != "::1" {
			fmt.Fprintln(os.Stderr, "WARNING: serving plain HTTP on a public interface — passwords and files travel unencrypted.")
		}
	}
	fmt.Printf("Skiff %s web UI on %s://%s  (user %q)\n", version, scheme, ln.Addr(), name)
	if cert != "" && *certFile == "" {
		fmt.Printf("Self-signed certificate SHA-256: %s\n", certFingerprint(cert))
	}
	if *root != "" {
		fmt.Printf("Local panel restricted to %s\n", *root)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := srv.Serve(ctx, ln, cert, key); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// selfSigned returns (and creates on first use) a self-signed certificate.
func selfSigned(dir string) (string, string, error) {
	cert, key := filepath.Join(dir, "web-cert.pem"), filepath.Join(dir, "web-key.pem")
	if _, err := os.Stat(cert); err == nil {
		if _, err := os.Stat(key); err == nil {
			return cert, key, nil
		}
	}
	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	host, _ := os.Hostname()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "Skiff on " + host, Organization: []string{"Skiff"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				tpl.IPAddresses = append(tpl.IPAddresses, ipn.IP)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &pk.PublicKey, pk)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalECPrivateKey(pk)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return "", "", err
	}
	return cert, key, nil
}

func certFingerprint(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "?"
	}
	b, _ := pem.Decode(data)
	if b == nil {
		return "?"
	}
	sum := sha256.Sum256(b.Bytes)
	parts := make([]string, len(sum))
	for i, x := range sum {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, ":")
}

var _ = bufio.NewReader
