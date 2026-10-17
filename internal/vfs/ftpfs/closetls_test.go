package ftpfs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"testing"
	"time"
)

func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// An upload on a TLS 1.3 data connection never reads the session tickets
// the server sends after the handshake. Closing with them unread resets the
// connection, and the system then drops what it has not sent yet: the end
// of the file. The server below reads late, as a busy or distant one does,
// so the end is still waiting to be sent when the upload closes.
func TestCloseTLSKeepsTheEndOfAnUpload(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srvCfg := &tls.Config{Certificates: []tls.Certificate{testCert(t)}, MinVersion: tls.VersionTLS13}
	type result struct {
		data []byte
		err  error
	}
	got := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- result{err: err}
			return
		}
		defer c.Close()
		tc := tls.Server(c, srvCfg)
		if err := tc.Handshake(); err != nil {
			got <- result{err: err}
			return
		}
		// a slow reader: the client is done writing well before the end
		// of the file has gone through
		var b []byte
		buf := make([]byte, 64<<10)
		for {
			n, err := tc.Read(buf)
			b = append(b, buf[:n]...)
			if err == io.EOF {
				break
			}
			if err != nil {
				got <- result{b, err}
				return
			}
			time.Sleep(time.Millisecond)
		}
		got <- result{b, nil}
	}()

	nc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// a session cache, as the client has: the server then sends tickets
	tc := tls.Client(nc, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, ClientSessionCache: tls.NewLRUClientSessionCache(1)})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("skiff-upload "), 1<<20) // 13 MB
	if _, err := tc.Write(payload); err != nil {
		t.Fatal(err)
	}
	closeTLS(tc)
	tc.Close()

	r := <-got
	if r.err != nil {
		t.Fatalf("server read: %v", r.err)
	}
	if !bytes.Equal(r.data, payload) {
		t.Fatalf("server got %d bytes, want %d", len(r.data), len(payload))
	}
}
