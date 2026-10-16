// Package netx provides the network plumbing shared by every protocol:
// proxies (SOCKS4/5, HTTP CONNECT, Telnet, local command), dialing through
// SSH tunnels, and interactive TLS certificate verification.
package netx

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Dialer opens network connections.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// DialFunc adapts a function to Dialer.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func (f DialFunc) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return f(ctx, network, addr)
}

// ProxyConfig mirrors WinSCP's proxy settings.
type ProxyConfig struct {
	Type      string `json:"type"` // "", "socks4", "socks5", "http", "telnet", "command"
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	User      string `json:"user,omitempty"`
	Password  string `json:"password,omitempty"`
	Command   string `json:"command,omitempty"` // telnet command or local command line
	RemoteDNS bool   `json:"remoteDns,omitempty"`
	// Expanded marks a command whose placeholders are already replaced
	// (an OpenSSH ProxyCommand).
	Expanded bool `json:"-"`
}

func (p *ProxyConfig) Enabled() bool { return p != nil && p.Type != "" && p.Type != "none" }

// Options for the base dialer.
type Options struct {
	Timeout   time.Duration
	IPVersion string // "", "4", "6"
	Proxy     *ProxyConfig
	// Via, when set, is used instead of a direct TCP dial (SSH tunnel).
	Via Dialer
}

// New builds a Dialer honouring the timeout, IP version, proxy and tunnel.
func New(o Options) Dialer {
	base := o.Via
	if base == nil {
		nd := &net.Dialer{Timeout: o.Timeout, KeepAlive: 30 * time.Second}
		base = DialFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
			switch o.IPVersion {
			case "4":
				network = "tcp4"
			case "6":
				network = "tcp6"
			}
			return nd.DialContext(ctx, network, addr)
		})
	}
	if !o.Proxy.Enabled() {
		return base
	}
	p := *o.Proxy
	return DialFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
		if o.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, o.Timeout)
			defer cancel()
		}
		return dialProxy(ctx, base, &p, addr)
	})
}

func dialProxy(ctx context.Context, base Dialer, p *ProxyConfig, addr string) (net.Conn, error) {
	if p.Type == "command" {
		return dialCommand(ctx, p, addr)
	}
	port := p.Port
	if port == 0 {
		switch p.Type {
		case "socks4", "socks5":
			port = 1080
		case "http":
			port = 8080
		case "telnet":
			port = 23
		}
	}
	paddr := net.JoinHostPort(p.Host, strconv.Itoa(port))
	c, err := base.DialContext(ctx, "tcp", paddr)
	if err != nil {
		return nil, fmt.Errorf("proxy %s: %w", paddr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		c.SetDeadline(dl)
	}
	var out net.Conn
	switch p.Type {
	case "socks5":
		out, err = socks5(c, p, addr)
	case "socks4":
		out, err = socks4(c, p, addr)
	case "http":
		out, err = httpConnect(c, p, addr)
	case "telnet":
		out, err = telnetProxy(c, p, addr)
	default:
		err = fmt.Errorf("unknown proxy type %q", p.Type)
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	out.SetDeadline(time.Time{})
	return out, nil
}

func splitHostPort(addr string) (string, int, error) {
	h, ps, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(ps)
	return h, port, err
}

func socks5(c net.Conn, p *ProxyConfig, addr string) (net.Conn, error) {
	host, port, err := splitHostPort(addr)
	if err != nil {
		return nil, err
	}
	methods := []byte{0x00}
	if p.User != "" {
		methods = []byte{0x00, 0x02}
	}
	if _, err := c.Write(append([]byte{5, byte(len(methods))}, methods...)); err != nil {
		return nil, err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil {
		return nil, err
	}
	if buf[0] != 5 {
		return nil, errors.New("socks5: bad server version")
	}
	switch buf[1] {
	case 0x00:
	case 0x02:
		req := []byte{1, byte(len(p.User))}
		req = append(req, p.User...)
		req = append(req, byte(len(p.Password)))
		req = append(req, p.Password...)
		if _, err := c.Write(req); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(c, buf); err != nil {
			return nil, err
		}
		if buf[1] != 0 {
			return nil, errors.New("socks5: authentication failed")
		}
	default:
		return nil, errors.New("socks5: no acceptable authentication method")
	}
	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil && !p.RemoteDNS {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 1)
			req = append(req, ip4...)
		} else {
			req = append(req, 4)
			req = append(req, ip.To16()...)
		}
	} else if !p.RemoteDNS {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			req = append(req, 3, byte(len(host)))
			req = append(req, host...)
		} else if ip4 := ips[0].To4(); ip4 != nil {
			req = append(req, 1)
			req = append(req, ip4...)
		} else {
			req = append(req, 4)
			req = append(req, ips[0].To16()...)
		}
	} else {
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		return nil, err
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return nil, err
	}
	if hdr[1] != 0 {
		return nil, fmt.Errorf("socks5: connect failed (code %d)", hdr[1])
	}
	var skip int
	switch hdr[3] {
	case 1:
		skip = 4
	case 4:
		skip = 16
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return nil, err
		}
		skip = int(l[0])
	}
	if _, err := io.ReadFull(c, make([]byte, skip+2)); err != nil {
		return nil, err
	}
	return c, nil
}

func socks4(c net.Conn, p *ProxyConfig, addr string) (net.Conn, error) {
	host, port, err := splitHostPort(addr)
	if err != nil {
		return nil, err
	}
	req := []byte{4, 1, byte(port >> 8), byte(port)}
	var ip4 net.IP
	if ip := net.ParseIP(host); ip != nil {
		ip4 = ip.To4()
	} else if !p.RemoteDNS {
		if ips, err := net.LookupIP(host); err == nil {
			for _, ip := range ips {
				if ip.To4() != nil {
					ip4 = ip.To4()
					break
				}
			}
		}
	}
	socks4a := ip4 == nil
	if socks4a {
		req = append(req, 0, 0, 0, 1)
	} else {
		req = append(req, ip4...)
	}
	req = append(req, p.User...)
	req = append(req, 0)
	if socks4a {
		req = append(req, host...)
		req = append(req, 0)
	}
	if _, err := c.Write(req); err != nil {
		return nil, err
	}
	resp := make([]byte, 8)
	if _, err := io.ReadFull(c, resp); err != nil {
		return nil, err
	}
	if resp[1] != 0x5a {
		return nil, fmt.Errorf("socks4: request rejected (code %d)", resp[1])
	}
	return c, nil
}

// bufConn preserves bytes read ahead by a bufio.Reader.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func httpConnect(c net.Conn, p *ProxyConfig, addr string) (net.Conn, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if p.User != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(p.User + ":" + p.Password))
		fmt.Fprintf(&sb, "Proxy-Authorization: Basic %s\r\n", cred)
	}
	sb.WriteString("User-Agent: Skiff\r\n\r\n")
	if _, err := io.WriteString(c, sb.String()); err != nil {
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http proxy: %s", resp.Status)
	}
	return &bufConn{Conn: c, r: br}, nil
}

func expandProxyCommand(tpl string, p *ProxyConfig, addr string) string {
	host, port, _ := splitHostPort(addr)
	r := strings.NewReplacer(
		"%host", host, "%port", strconv.Itoa(port),
		"%user", p.User, "%pass", p.Password,
		"%proxyhost", p.Host, "%proxyport", strconv.Itoa(p.Port),
		`\n`, "\n", `\r`, "\r", "%%", "%",
	)
	return r.Replace(tpl)
}

func telnetProxy(c net.Conn, p *ProxyConfig, addr string) (net.Conn, error) {
	tpl := p.Command
	if tpl == "" {
		tpl = `connect %host %port\n`
	}
	if _, err := io.WriteString(c, expandProxyCommand(tpl, p, addr)); err != nil {
		return nil, err
	}
	return c, nil
}

// cmdConn adapts a child process' stdio into a net.Conn (ProxyCommand).
type cmdConn struct {
	cmd *exec.Cmd
	io.Reader
	io.WriteCloser
}

func (c *cmdConn) Close() error {
	c.WriteCloser.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

type cmdAddr struct{}

func (cmdAddr) Network() string { return "command" }
func (cmdAddr) String() string  { return "proxy-command" }

func (c *cmdConn) LocalAddr() net.Addr                { return cmdAddr{} }
func (c *cmdConn) RemoteAddr() net.Addr               { return cmdAddr{} }
func (c *cmdConn) SetDeadline(t time.Time) error      { return nil }
func (c *cmdConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *cmdConn) SetWriteDeadline(t time.Time) error { return nil }

func dialCommand(ctx context.Context, p *ProxyConfig, addr string) (net.Conn, error) {
	line := p.Command
	if !p.Expanded {
		line = expandProxyCommand(p.Command, p, addr)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", line)
	} else {
		cmd = exec.Command("/bin/sh", "-c", line)
	}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("proxy command: %w", err)
	}
	return &cmdConn{cmd: cmd, Reader: out, WriteCloser: in}, nil
}
