//go:build integration

package server

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/keys"
	"skiff/internal/prompt"
	"skiff/internal/sshx"
	"skiff/internal/vfs"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// readBanner connects to addr and returns the first line (SSH banner).
func readBanner(addr string) (string, error) {
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	return bufio.NewReader(c).ReadString('\n')
}

func (e *e2e) tunnel(t *testing.T, conn string, f config.Forward) core.TunnelInfo {
	t.Helper()
	var ti core.TunnelInfo
	e.must(t, "tunnel.start", map[string]any{"conn": conn, "forward": f}, &ti)
	if !ti.Running {
		t.Fatalf("tunnel not running: %+v", ti)
	}
	return ti
}

func (e *e2e) tunnelInfo(t *testing.T, id string) (core.TunnelInfo, bool) {
	t.Helper()
	var list []core.TunnelInfo
	e.must(t, "tunnel.list", nil, &list)
	for _, x := range list {
		if x.ID == id {
			return x, true
		}
	}
	return core.TunnelInfo{}, false
}

// socks5Dial opens host:port through a SOCKS5 proxy.
func socks5Dial(proxy, host string, port int) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte{5, 1, 0})
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil || buf[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5 greeting: %v %v", buf, err)
	}
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	c.Write(req)
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5 connect: %v %v", rep, err)
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

// socks4aDial opens host:port through a SOCKS4a proxy.
func socks4aDial(proxy, host string, port int) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	req := []byte{4, 1, byte(port >> 8), byte(port), 0, 0, 0, 1}
	req = append(req, "skiff"...)
	req = append(req, 0)
	req = append(req, host...)
	req = append(req, 0)
	c.Write(req)
	rep := make([]byte, 8)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0x5a {
		c.Close()
		return nil, fmt.Errorf("socks4a: %v %v", rep, err)
	}
	c.SetDeadline(time.Time{})
	return c, nil
}

func testTunnels(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	t.Run("local", func(t *testing.T) {
		port := freePort(t)
		ti := e.tunnel(t, ci.ID, config.Forward{Type: "local", BindHost: "127.0.0.1", BindPort: port, DestHost: "127.0.0.1", DestPort: 2222, Label: "sshd"})
		// the destination is seen from the server: its own sshd
		banner, err := readBanner(fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil || !strings.HasPrefix(banner, "SSH-2.0-OpenSSH") {
			t.Fatalf("through -L: %q %v", banner, err)
		}
		// an HTTP request to another container of the network
		port2 := freePort(t)
		e.tunnel(t, ci.ID, config.Forward{Type: "local", BindHost: "127.0.0.1", BindPort: port2, DestHost: "webdav", DestPort: 8080})
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port2))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(c, "GET / HTTP/1.0\r\nHost: webdav\r\n\r\n")
		line, _ := bufio.NewReader(c).ReadString('\n')
		c.Close()
		if !strings.HasPrefix(line, "HTTP/1.") {
			t.Errorf("HTTP through -L: %q", line)
		}
		eventually(t, "tunnel statistics", 5*time.Second, func() bool {
			info, _ := e.tunnelInfo(t, ti.ID)
			return info.Total >= 1 && info.BytesIn > 0 && info.Active == 0
		})
		e.must(t, "tunnel.stop", map[string]string{"id": ti.ID}, nil)
		if _, ok := e.tunnelInfo(t, ti.ID); ok {
			t.Error("stopped tunnel still listed")
		}
		if _, err := readBanner(fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			t.Error("stopped tunnel still accepts connections")
		}
	})
	t.Run("dynamicSocks", func(t *testing.T) {
		port := freePort(t)
		e.tunnel(t, ci.ID, config.Forward{Type: "dynamic", BindHost: "127.0.0.1", BindPort: port})
		proxy := fmt.Sprintf("127.0.0.1:%d", port)
		c, err := socks5Dial(proxy, "webdav", 8080)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(c, "GET / HTTP/1.0\r\nHost: webdav\r\n\r\n")
		line, _ := bufio.NewReader(c).ReadString('\n')
		c.Close()
		if !strings.HasPrefix(line, "HTTP/1.") {
			t.Errorf("HTTP through SOCKS5: %q", line)
		}
		c, err = socks4aDial(proxy, "localhost", 2222)
		if err != nil {
			t.Fatal(err)
		}
		line, _ = bufio.NewReader(c).ReadString('\n')
		c.Close()
		if !strings.HasPrefix(line, "SSH-2.0") {
			t.Errorf("SOCKS4a: %q", line)
		}
	})
	t.Run("remote", func(t *testing.T) {
		// a local service reachable from the server through -R
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				io.WriteString(c, "BONJOUR-DU-POSTE\n")
				c.Close()
			}
		}()
		rport := 18000 + int(time.Now().UnixNano()%1000)
		e.tunnel(t, ci.ID, config.Forward{Type: "remote", BindHost: "127.0.0.1", BindPort: rport, DestHost: "127.0.0.1", DestPort: l.Addr().(*net.TCPAddr).Port})
		var res core.ExecResult
		e.must(t, "fs.exec", map[string]string{"conn": ci.ID, "command": fmt.Sprintf("nc -w 3 127.0.0.1 %d </dev/null", rport), "cwd": "/"}, &res)
		if !strings.Contains(res.Stdout, "BONJOUR-DU-POSTE") {
			t.Errorf("through -R: %+v", res)
		}
	})
	t.Run("autoStartAndClose", func(t *testing.T) {
		port := freePort(t)
		s := testSite("sftp")
		s.Forwards = []config.Forward{{ID: "f1", Type: "local", BindHost: "127.0.0.1", BindPort: port, DestHost: "127.0.0.1", DestPort: 2222, AutoStart: true, Label: "auto"}, {ID: "f2", Type: "dynamic", BindPort: freePort(t)}}
		c2 := e.open(t, e.saveSite(t, s, "skiff").ID)
		var list []core.TunnelInfo
		e.must(t, "tunnel.list", nil, &list)
		n := 0
		for _, x := range list {
			if x.Conn == c2.ID {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("auto-started tunnels = %d, want 1", n)
		}
		if b, err := readBanner(fmt.Sprintf("127.0.0.1:%d", port)); err != nil || !strings.HasPrefix(b, "SSH-2.0") {
			t.Errorf("auto tunnel: %q %v", b, err)
		}
		e.must(t, "conn.close", map[string]string{"conn": c2.ID}, nil)
		e.must(t, "tunnel.list", nil, &list)
		for _, x := range list {
			if x.Conn == c2.ID {
				t.Error("tunnel survived its connection")
			}
		}
	})
	t.Run("notSSH", func(t *testing.T) {
		f := e.connect(t, "webdav")
		e.mustFail(t, "tunnel.start", map[string]any{"conn": f.ID, "forward": config.Forward{Type: "local", BindPort: freePort(t), DestHost: "x", DestPort: 1}})
	})
}

func testJumpHosts(t *testing.T, e *e2e) {
	bastion := e.saveSite(t, testSite("sftp"), "skiff")
	jumpInfo := func(t *testing.T, conn string) string {
		var info []vfs.InfoItem
		e.must(t, "conn.serverInfo", map[string]string{"conn": conn}, &info)
		for _, i := range info {
			if i.Key == "Jump hosts" {
				return i.Value
			}
		}
		return ""
	}
	t.Run("savedSite", func(t *testing.T) {
		s := testSite("sftp")
		s.Host = "localhost" // as seen from the bastion
		s.Jumps = []config.Jump{{SiteID: bastion.ID}}
		ci := e.open(t, e.saveSite(t, s, "skiff").ID)
		if jumpInfo(t, ci.ID) != "1" {
			t.Errorf("jump hosts = %s", jumpInfo(t, ci.ID))
		}
		root := e.remoteRoot(t, ci)
		e.must(t, "fs.createFile", map[string]any{"conn": ci.ID, "path": root + "/via-rebond.txt", "content": "ok"}, nil)
	})
	t.Run("twoHopsWithCustomHost", func(t *testing.T) {
		s := testSite("sftp")
		s.Host = "localhost"
		s.Jumps = []config.Jump{{SiteID: bastion.ID}, {Host: "127.0.0.1", Port: 2222, User: "skiff"}}
		pw := "skiff"
		var v config.SiteView
		e.must(t, "sites.save", map[string]any{"site": s, "secrets": config.SecretInput{Password: &pw, JumpPasswords: map[int]*string{1: &pw}}}, &v)
		if len(v.JumpHasPassword) != 2 || !v.JumpHasPassword[1] {
			t.Errorf("jump password flags = %v", v.JumpHasPassword)
		}
		ci := e.open(t, v.ID)
		if jumpInfo(t, ci.ID) != "2" {
			t.Errorf("jump hosts = %s", jumpInfo(t, ci.ID))
		}
	})
	for _, proto := range []string{"ftp", "webdav", "s3"} {
		proto := proto
		t.Run(proto+"ThroughSSH", func(t *testing.T) {
			s := testSite(proto)
			s.Jumps = []config.Jump{{SiteID: bastion.ID}}
			switch proto {
			case "ftp": // reached as ftp:21 from the bastion, passive ports included
				s.Host, s.Port, s.FTPForcePasvIP = "ftp", 21, "on"
			case "webdav":
				s.Host = "webdav"
			case "s3":
				s.Host = "s3"
			}
			ci := e.open(t, e.saveSite(t, s, testPassword(proto)).ID)
			root := e.remoteRoot(t, ci)
			e.upload(t, ci.ID, root+"/rebond.txt", []byte("via le bastion"), true)
			if got := e.readText(t, ci.ID, root+"/rebond.txt"); got != "via le bastion" {
				t.Errorf("content = %q", got)
			}
		})
	}
}

// httpConnectProxy is a minimal authenticated HTTP CONNECT proxy.
func httpConnectProxy(t *testing.T, user, pass string, hits *int32) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				line, _ := br.ReadString('\n')
				auth := ""
				for {
					h, err := br.ReadString('\n')
					if err != nil || h == "\r\n" {
						break
					}
					if strings.HasPrefix(strings.ToLower(h), "proxy-authorization:") {
						auth = strings.TrimSpace(h[len("proxy-authorization:"):])
					}
				}
				f := strings.Fields(line)
				if len(f) < 2 || f[0] != "CONNECT" || auth != want {
					io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
					return
				}
				d, err := net.Dial("tcp", f[1])
				if err != nil {
					io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
					return
				}
				atomic.AddInt32(hits, 1)
				io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
				go io.Copy(d, br)
				io.Copy(c, d)
				d.Close()
			}()
		}
	}()
	return l.Addr().String()
}

// telnetProxy is a minimal "connect host port" proxy.
func fakeTelnetProxy(t *testing.T, hits *int32) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				line, _ := br.ReadString('\n')
				f := strings.Fields(line)
				if len(f) != 3 || f[0] != "connect" {
					return
				}
				d, err := net.Dial("tcp", net.JoinHostPort(f[1], f[2]))
				if err != nil {
					return
				}
				atomic.AddInt32(hits, 1)
				go io.Copy(d, br)
				io.Copy(c, d)
				d.Close()
			}()
		}
	}()
	return l.Addr().String()
}

func testProxies(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	socksPort := freePort(t)
	e.tunnel(t, ci.ID, config.Forward{Type: "dynamic", BindHost: "127.0.0.1", BindPort: socksPort})
	connectVia := func(t *testing.T, s config.Site, proxyPass string) {
		t.Helper()
		pw := "skiff"
		sec := config.SecretInput{Password: &pw}
		if proxyPass != "" {
			sec.ProxyPassword = &proxyPass
		}
		var v config.SiteView
		e.must(t, "sites.save", map[string]any{"site": s, "secrets": sec}, &v)
		c := e.open(t, v.ID)
		if err := e.call("fs.list", map[string]string{"conn": c.ID, "path": c.Home}, nil); err != nil {
			t.Fatal(err)
		}
		e.must(t, "conn.close", map[string]string{"conn": c.ID}, nil)
	}
	t.Run("socks5", func(t *testing.T) {
		s := testSite("sftp")
		s.Proxy = config.Proxy{Type: "socks5", Host: "127.0.0.1", Port: socksPort}
		connectVia(t, s, "")
	})
	t.Run("socks5RemoteDNS", func(t *testing.T) {
		s := testSite("sftp")
		s.Host = "localhost"
		s.Proxy = config.Proxy{Type: "socks5", Host: "127.0.0.1", Port: socksPort, RemoteDNS: true}
		connectVia(t, s, "")
	})
	t.Run("socks4", func(t *testing.T) {
		s := testSite("sftp")
		s.Proxy = config.Proxy{Type: "socks4", Host: "127.0.0.1", Port: socksPort}
		connectVia(t, s, "")
	})
	t.Run("httpConnect", func(t *testing.T) {
		var hits int32
		addr := httpConnectProxy(t, "proxyuser", "proxypass", &hits)
		host, port, _ := net.SplitHostPort(addr)
		p, _ := strconv.Atoi(port)
		s := testSite("sftp")
		s.Proxy = config.Proxy{Type: "http", Host: host, Port: p, User: "proxyuser"}
		connectVia(t, s, "proxypass")
		if atomic.LoadInt32(&hits) == 0 {
			t.Error("the HTTP proxy was not used")
		}
		// wrong proxy password
		s.ID = config.NewID()
		bad := "faux"
		var v config.SiteView
		pw := "skiff"
		e.must(t, "sites.save", map[string]any{"site": s, "secrets": config.SecretInput{Password: &pw, ProxyPassword: &bad}}, &v)
		e.mustFail(t, "conn.open", map[string]any{"siteId": v.ID})
	})
	t.Run("telnet", func(t *testing.T) {
		var hits int32
		addr := fakeTelnetProxy(t, &hits)
		host, port, _ := net.SplitHostPort(addr)
		p, _ := strconv.Atoi(port)
		s := testSite("sftp")
		s.Proxy = config.Proxy{Type: "telnet", Host: host, Port: p}
		connectVia(t, s, "")
		if atomic.LoadInt32(&hits) == 0 {
			t.Error("the telnet proxy was not used")
		}
	})
	t.Run("command", func(t *testing.T) {
		s := testSite("sftp")
		s.Proxy = config.Proxy{Type: "command", Command: "nc %host %port"}
		connectVia(t, s, "")
	})
	t.Run("proxyForFTP", func(t *testing.T) {
		s := testSite("webdav")
		s.Proxy = config.Proxy{Type: "socks5", Host: "127.0.0.1", Port: socksPort}
		s.Host = "webdav" // resolved by the proxy (on the SSH server)
		s.Proxy.RemoteDNS = true
		pw := "skiff"
		var v config.SiteView
		e.must(t, "sites.save", map[string]any{"site": s, "secrets": config.SecretInput{Password: &pw}}, &v)
		c := e.open(t, v.ID)
		e.must(t, "fs.list", map[string]string{"conn": c.ID, "path": "/"}, nil)
	})
}

func testKeys(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	ssh := filepath.Join(e.home, ".ssh")
	gen := func(t *testing.T, r keys.GenerateRequest) keys.Generated {
		t.Helper()
		var g keys.Generated
		e.must(t, "keys.generate", r, &g)
		if g.Fingerprint == "" || !strings.HasPrefix(g.Public, "ssh-") && !strings.HasPrefix(g.Public, "ecdsa-") {
			t.Fatalf("generated = %+v", g)
		}
		return g
	}
	ed := gen(t, keys.GenerateRequest{Type: "ed25519", Comment: "e2e@test", Format: "openssh", SaveTo: filepath.Join(ssh, "id_e2e")})
	rsa := gen(t, keys.GenerateRequest{Type: "rsa", Bits: 2048, Comment: "rsa", Passphrase: "phrase-secrete", Format: "ppk", SaveTo: filepath.Join(ssh, "id_rsa_e2e.ppk")})
	ec := gen(t, keys.GenerateRequest{Type: "ecdsa", Bits: 384, Format: "openssh"}) // in memory only
	if ec.Path != "" || !strings.Contains(ec.Private, "PRIVATE KEY") {
		t.Errorf("unsaved key = %+v", ec)
	}
	if err := e.call("keys.generate", keys.GenerateRequest{Type: "ed25519", Format: "openssh", SaveTo: filepath.Join(ssh, "id_e2e")}, nil); err == nil {
		t.Error("an existing key file must never be overwritten")
	}
	if _, err := os.Stat(filepath.Join(ssh, "id_e2e.pub")); err != nil {
		t.Error("public key file not written")
	}
	var list []keys.KeyFile
	e.must(t, "keys.list", nil, &list)
	formats := map[string]keys.KeyFile{}
	for _, k := range list {
		formats[filepath.Base(k.Path)] = k
	}
	if formats["id_e2e"].Format != "openssh" || formats["id_rsa_e2e.ppk"].Format != "ppk" || !formats["id_rsa_e2e.ppk"].Encrypted || formats["id_e2e"].Fingerprint != ed.Fingerprint {
		t.Errorf("keys.list = %+v", list)
	}
	// the listed public keys, even of an encrypted key: what the install
	// dialog offers
	sameKey := func(a, b string) bool {
		f, g := strings.Fields(a), strings.Fields(b)
		return len(f) > 1 && len(g) > 1 && f[0] == g[0] && f[1] == g[1]
	}
	if !sameKey(formats["id_e2e"].PublicKey, ed.Public) || !strings.HasSuffix(formats["id_e2e"].PublicKey, " e2e@test") {
		t.Errorf("public key of id_e2e %q, want %q", formats["id_e2e"].PublicKey, ed.Public)
	}
	if !sameKey(formats["id_rsa_e2e.ppk"].PublicKey, rsa.Public) {
		t.Errorf("public key of the encrypted .ppk %q, want %q", formats["id_rsa_e2e.ppk"].PublicKey, rsa.Public)
	}
	// install the public keys, then log in with the keys only (they go
	// at the end: the test server keeps its authorized_keys between runs)
	t.Cleanup(func() {
		e.call("keys.removeAuthorized", map[string]any{"conn": ci.ID, "fingerprints": []string{ed.Fingerprint, rsa.Fingerprint}, "force": true}, nil)
	})
	var where string
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": ed.Public}, &where)
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": formats["id_rsa_e2e.ppk"].PublicKey}, &where)
	e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": formats["id_e2e"].PublicKey}, &where) // already there
	if !strings.HasSuffix(where, ".ssh/authorized_keys") {
		t.Errorf("installed in %s", where)
	}
	if n := strings.Count(e.readText(t, ci.ID, where), strings.Fields(ed.Public)[1]); n != 1 {
		t.Errorf("the key is %d times in authorized_keys", n)
	}
	keyOnly := func(keyFile, passphrase string) config.Site {
		s := testSite("sftp")
		s.KeyFile, s.KbdInteractive = keyFile, false
		return s
	}
	t.Run("openssh", func(t *testing.T) {
		c := e.open(t, e.saveSite(t, keyOnly("~/.ssh/id_e2e", ""), "").ID)
		var lines []core.LogLine
		e.must(t, "conn.log", map[string]string{"conn": c.ID}, &lines)
		ok := false
		for _, l := range lines {
			ok = ok || strings.Contains(l.Msg, "publickey")
		}
		if !ok {
			t.Errorf("not authenticated by key: %+v", lines)
		}
	})
	t.Run("ppkWithSavedPassphrase", func(t *testing.T) {
		s := keyOnly(filepath.Join(ssh, "id_rsa_e2e.ppk"), "")
		pp := "phrase-secrete"
		var v config.SiteView
		e.must(t, "sites.save", map[string]any{"site": s, "secrets": config.SecretInput{Passphrase: &pp}}, &v)
		if !v.HasPassphrase {
			t.Error("passphrase not saved")
		}
		e.open(t, v.ID)
	})
	t.Run("ppkPassphrasePrompt", func(t *testing.T) {
		e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
			if ev.Kind == prompt.Passphrase {
				return prompt.Response{Choice: "ok", Values: []string{"phrase-secrete"}}, true
			}
			return defaultPolicy(ev)
		})
		defer e.setPolicy(defaultPolicy)
		n := len(e.promptsOf(prompt.Passphrase))
		e.open(t, e.saveSite(t, keyOnly(filepath.Join(ssh, "id_rsa_e2e.ppk"), ""), "").ID)
		if len(e.promptsOf(prompt.Passphrase)) != n+1 {
			t.Error("passphrase not asked")
		}
	})
	t.Run("convert", func(t *testing.T) {
		var g keys.Generated
		e.must(t, "keys.convert", keys.ConvertRequest{Path: filepath.Join(ssh, "id_e2e"), Format: "ppk", SaveTo: filepath.Join(ssh, "id_e2e.ppk")}, &g)
		if g.Fingerprint != ed.Fingerprint {
			t.Errorf("conversion changed the key: %s vs %s", g.Fingerprint, ed.Fingerprint)
		}
		e.open(t, e.saveSite(t, keyOnly(filepath.Join(ssh, "id_e2e.ppk"), ""), "").ID)
		e.must(t, "keys.convert", keys.ConvertRequest{Path: filepath.Join(ssh, "id_rsa_e2e.ppk"), Passphrase: "phrase-secrete", Format: "openssh", SaveTo: filepath.Join(ssh, "id_rsa_e2e_openssh")}, &g)
		e.open(t, e.saveSite(t, keyOnly(filepath.Join(ssh, "id_rsa_e2e_openssh"), ""), "").ID)
		e.mustFail(t, "keys.convert", keys.ConvertRequest{Path: filepath.Join(ssh, "id_rsa_e2e.ppk"), Passphrase: "faux", Format: "openssh"})
	})
	// the keys a server lets in: list, remove, replace after a check
	t.Run("authorizedKeys", func(t *testing.T) {
		find := func(au keys.Authorized, fp string) *keys.AuthorizedKey {
			for i := range au.Keys {
				if au.Keys[i].Fingerprint == fp {
					return &au.Keys[i]
				}
			}
			return nil
		}
		a := gen(t, keys.GenerateRequest{Type: "ed25519", Comment: "rotation a", Format: "openssh", SaveTo: filepath.Join(ssh, "id_rotation_a")})
		e.must(t, "keys.install", map[string]string{"conn": ci.ID, "publicKey": a.Public}, nil)
		var au keys.Authorized
		e.must(t, "keys.authorized", map[string]string{"conn": ci.ID}, &au)
		if k := find(au, a.Fingerprint); !au.Exists || k == nil || k.Comment != "rotation a" || k.Type != "ssh-ed25519" || au.AuthKey != "" || au.Version == "" {
			t.Fatalf("authorized (password connection): %+v", au)
		}
		if find(au, ed.Fingerprint) == nil {
			t.Error("the keys installed before are listed too")
		}
		// a connection that logged in with key a knows it
		byKey := e.open(t, e.saveSite(t, keyOnly(filepath.Join(ssh, "id_rotation_a"), ""), "").ID)
		e.must(t, "keys.authorized", map[string]string{"conn": byKey.ID}, &au)
		if au.AuthKey != a.Fingerprint || au.AuthCert {
			t.Fatalf("key of the connection %q, want %q", au.AuthKey, a.Fingerprint)
		}
		var info []map[string]string
		e.must(t, "conn.serverInfo", map[string]string{"conn": byKey.ID}, &info)
		shown := false
		for _, i := range info {
			shown = shown || (i["key"] == "Authentication key" && i["value"] == a.Fingerprint)
		}
		if !shown {
			t.Errorf("server info lacks the authentication key: %v", info)
		}
		// not without saying so: its own key, or from an older listing
		if err := e.call("keys.removeAuthorized", map[string]any{"conn": byKey.ID, "fingerprints": []string{a.Fingerprint}, "version": au.Version}, nil); err == nil || !strings.Contains(err.Error(), "logged in with this key") {
			t.Errorf("removing the key of the connection: %v", err)
		}
		if err := e.call("keys.removeAuthorized", map[string]any{"conn": byKey.ID, "fingerprints": []string{a.Fingerprint}, "version": "0000000000000000", "force": true}, nil); err == nil || !strings.Contains(err.Error(), "changed since") {
			t.Errorf("removing from an older listing: %v", err)
		}
		// replacement: b is checked by a second connection, then a goes
		b := gen(t, keys.GenerateRequest{Type: "ed25519", Comment: "rotation b", Format: "openssh", SaveTo: filepath.Join(ssh, "id_rotation_b")})
		var res keys.ReplaceResult
		e.must(t, "keys.replace", map[string]string{"conn": byKey.ID, "publicKey": b.Public, "oldFingerprint": a.Fingerprint}, &res)
		if !res.Added || !res.Verified || res.Removed != 1 {
			t.Errorf("replace = %+v", res)
		}
		e.must(t, "keys.authorized", map[string]string{"conn": ci.ID}, &au)
		if find(au, a.Fingerprint) != nil || find(au, b.Fingerprint) == nil || find(au, ed.Fingerprint) == nil {
			t.Fatalf("after the replacement: %+v", au)
		}
		e.open(t, e.saveSite(t, keyOnly(filepath.Join(ssh, "id_rotation_b"), ""), "").ID)
		// a key whose private part is not here: installed, the old one stays
		c := gen(t, keys.GenerateRequest{Type: "ed25519", Comment: "ailleurs", Format: "openssh"})
		if err := e.call("keys.replace", map[string]string{"conn": ci.ID, "publicKey": c.Public, "oldFingerprint": b.Fingerprint}, &res); err == nil || !strings.Contains(err.Error(), "old one stays") {
			t.Errorf("replacement with an unavailable key: %v", err)
		}
		e.must(t, "keys.authorized", map[string]string{"conn": ci.ID}, &au)
		if find(au, b.Fingerprint) == nil || find(au, c.Fingerprint) == nil {
			t.Fatalf("after the refused replacement: %+v", au)
		}
		// clean up: only those lines go
		before := len(au.Keys)
		e.must(t, "keys.removeAuthorized", map[string]any{"conn": ci.ID, "fingerprints": []string{b.Fingerprint, c.Fingerprint}, "version": au.Version}, &au)
		if find(au, b.Fingerprint) != nil || find(au, c.Fingerprint) != nil || len(au.Keys) != before-2 || find(au, ed.Fingerprint) == nil {
			t.Errorf("after the removal: %+v", au)
		}
		if strings.Contains(e.readText(t, ci.ID, au.Path), "skiff-new") || e.exists(ci.ID, au.Path+".skiff-new") {
			t.Error("temporary file left")
		}
	})
	t.Run("wrongKeyFallsBackToPassword", func(t *testing.T) {
		s := testSite("sftp")
		s.KeyFile = filepath.Join(ssh, "id_rsa_e2e_openssh")
		gen(t, keys.GenerateRequest{Type: "ed25519", Format: "openssh", SaveTo: filepath.Join(ssh, "id_pas_installee")})
		s.KeyFile = filepath.Join(ssh, "id_pas_installee")
		e.open(t, e.saveSite(t, s, "skiff").ID)
	})
}

func testHostKeys(t *testing.T, e *e2e) {
	known := func(host string) *sshx.KnownHost {
		var list []sshx.KnownHost
		e.must(t, "hostkeys.list", nil, &list)
		for _, h := range list {
			if strings.Contains(h.Hosts, "["+host+"]:2222") {
				return &h
			}
		}
		return nil
	}
	if known("127.0.0.1") == nil {
		t.Fatal("server not in known hosts")
	}
	asked := func() int { return len(e.promptsOf(prompt.HostKey)) }
	answer := func(choice string) {
		e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
			if ev.Kind == prompt.HostKey {
				return prompt.Response{Choice: choice}, true
			}
			return defaultPolicy(ev)
		})
	}
	defer e.setPolicy(defaultPolicy)
	// "localhost" is a separate known_hosts entry for the same server
	s := testSite("sftp")
	s.Host = "localhost"
	v := e.saveSite(t, s, "skiff")
	t.Run("cancelOnceAccept", func(t *testing.T) {
		if h := known("localhost"); h != nil {
			e.must(t, "hostkeys.remove", map[string]int{"line": h.Line}, nil)
		}
		if known("localhost") != nil {
			t.Fatal("not removed")
		}
		answer("cancel")
		n := asked()
		e.mustFail(t, "conn.open", map[string]any{"siteId": v.ID})
		if asked() != n+1 {
			t.Fatal("unknown key not asked")
		}
		// "connect once": not saved, but not asked again until Skiff restarts
		answer("once")
		e.open(t, v.ID)
		e.open(t, v.ID)
		if asked() != n+2 || known("localhost") != nil {
			t.Errorf("once: %d prompts, saved = %v", asked()-n, known("localhost") != nil)
		}
	})
	t.Run("acceptSaves", func(t *testing.T) {
		h := known("127.0.0.1")
		e.must(t, "hostkeys.remove", map[string]int{"line": h.Line}, nil)
		answer("accept")
		n := asked()
		e.open(t, e.saveSite(t, testSite("sftp"), "skiff").ID)
		if asked() != n+1 || known("127.0.0.1") == nil {
			t.Fatal("accepted key not saved")
		}
	})
	t.Run("changedKey", func(t *testing.T) {
		// replace the saved key with another one: the server now looks like an impostor
		var g keys.Generated
		e.must(t, "keys.generate", keys.GenerateRequest{Type: "ecdsa", Bits: 256, Format: "openssh"}, &g)
		kh := filepath.Join(e.cfg, "known_hosts")
		data, _ := os.ReadFile(kh)
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.Contains(line, "[127.0.0.1]:2222") {
				f, pub := strings.Fields(line), strings.Fields(g.Public)
				line = f[0] + " " + pub[0] + " " + pub[1]
			}
			out = append(out, line)
		}
		os.WriteFile(kh, []byte(strings.Join(out, "\n")+"\n"), 0o600)
		site := e.saveSite(t, testSite("sftp"), "skiff")
		answer("cancel")
		e.mustFail(t, "conn.open", map[string]any{"siteId": site.ID})
		last := e.promptsOf(prompt.HostKey)
		if p := last[len(last)-1]; !p.Danger || !strings.Contains(p.Details["previous"], ssh256(g.Public)) {
			t.Errorf("changed key prompt = %+v", p)
		}
		answer("accept") // "update the key"
		e.open(t, site.ID)
		n := asked()
		answer("cancel")
		e.open(t, site.ID)
		if asked() != n {
			t.Error("updated key still asked")
		}
	})
	t.Run("pinned", func(t *testing.T) {
		fp := known("127.0.0.1").Fingerprint
		e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
			if ev.Kind == prompt.HostKey {
				t.Errorf("no question expected with a pinned key")
				return prompt.Response{Choice: "cancel"}, true
			}
			return defaultPolicy(ev)
		})
		ps := testSite("sftp")
		ps.HostKey = fp
		e.open(t, e.saveSite(t, ps, "skiff").ID)
		ps.ID, ps.HostKey = config.NewID(), "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		if err := e.call("conn.open", map[string]any{"siteId": e.saveSite(t, ps, "skiff").ID}, nil); err == nil {
			t.Error("a wrong pinned key must be refused")
		}
	})
}

// ssh256 returns the SHA-256 fingerprint of an authorized_keys line.
func ssh256(pub string) string {
	k, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pub))
	if err != nil {
		return "?"
	}
	return gossh.FingerprintSHA256(k)
}

func testTLS(t *testing.T, e *e2e) {
	certs := func() int { return len(e.promptsOf(prompt.Certificate)) }
	n := certs()
	// "trust once" asks every time
	e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
		if ev.Kind == prompt.Certificate {
			return prompt.Response{Choice: "once"}, true
		}
		return defaultPolicy(ev)
	})
	// the FTPS server has already been trusted "always" by earlier tests:
	// use the IPv6/localhost name to get a new host entry
	s := testSite("ftp")
	s.Host = "localhost"
	v := e.saveSite(t, s, "skiff")
	c := e.open(t, v.ID)
	e.must(t, "conn.close", map[string]string{"conn": c.ID}, nil)
	c = e.open(t, v.ID)
	e.must(t, "conn.close", map[string]string{"conn": c.ID}, nil)
	if certs() != n+2 {
		t.Errorf("trust once: %d prompts", certs()-n)
	}
	last := e.promptsOf(prompt.Certificate)
	fp := last[len(last)-1].Details["sha256"]
	if fp == "" || last[len(last)-1].Details["subject"] == "" {
		t.Fatalf("certificate details = %v", last[len(last)-1].Details)
	}
	// pinned fingerprint: no question; a wrong one is refused
	e.setPolicy(func(ev core.PromptEvent) (prompt.Response, bool) {
		if ev.Kind == prompt.Certificate {
			t.Errorf("no certificate question expected")
			return prompt.Response{Choice: "cancel"}, true
		}
		return defaultPolicy(ev)
	})
	p := s
	p.ID, p.TLSFingerprint = config.NewID(), fp
	c = e.open(t, e.saveSite(t, p, "skiff").ID)
	e.must(t, "conn.close", map[string]string{"conn": c.ID}, nil)
	p.ID, p.TLSFingerprint = config.NewID(), "AA:BB"
	e.mustFail(t, "conn.open", map[string]any{"siteId": e.saveSite(t, p, "skiff").ID})
	// insecure: no verification at all
	p.ID, p.TLSFingerprint, p.TLSInsecure = config.NewID(), "", true
	c = e.open(t, e.saveSite(t, p, "skiff").ID)
	e.must(t, "conn.close", map[string]string{"conn": c.ID}, nil)
	// "always trust": asked once, then remembered
	e.setPolicy(defaultPolicy)
	n = certs()
	for i := 0; i < 2; i++ {
		c = e.open(t, v.ID)
		e.must(t, "conn.close", map[string]string{"conn": c.ID}, nil)
	}
	if certs() != n+1 {
		t.Errorf("always trust: %d prompts", certs()-n)
	}
	if _, err := os.Stat(filepath.Join(e.cfg, "trusted_certs.json")); err != nil {
		t.Error("trusted_certs.json not written")
	}
	// TLS information in the server info
	c = e.open(t, v.ID)
	var info []vfs.InfoItem
	e.must(t, "conn.serverInfo", map[string]string{"conn": c.ID}, &info)
	keys := []string{}
	for _, i := range info {
		if i.Group == "TLS" {
			keys = append(keys, i.Key)
		}
	}
	if strings.Join(keys, ",") != "cipher,expires,fingerprint,issuer,subject,version" {
		t.Errorf("TLS info keys = %v", keys)
	}
}
