// Package ftpfs implements vfs.FS over FTP, FTPS (explicit and implicit TLS)
// with passive and active modes, MLSD/LIST parsing and raw commands.
package ftpfs

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

	"skiff/internal/netx"
)

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Account  string
	TLS      string // "none", "explicit", "implicit"
	// Passive selects passive mode (default). Active mode makes the server
	// connect back to us (PORT/EPRT).
	Passive bool
	// ActiveIP overrides the address announced in active mode.
	ActiveIP string
	// ForcePasvIP: "auto" (default), "on" use control host, "off" trust PASV reply.
	ForcePasvIP string
	DisableEPSV bool
	UseMLSD     string // "auto", "on", "off"
	ListHidden  bool   // use "LIST -a"
	Encoding    string // "" / "utf-8" or a legacy charset name
	Timeout     time.Duration
	Dialer      netx.Dialer
	TLSVerifier *netx.TLSVerifier
	// ViaProxy is true when Dialer goes through a proxy or tunnel (no active mode).
	ViaProxy bool
	Logf     func(string, ...any)
}

// Response is a parsed FTP reply.
type Response struct {
	Code int
	Msg  string
}

func (r Response) Error() string { return fmt.Sprintf("%d %s", r.Code, r.Msg) }

// ProtoError is returned for negative FTP replies.
type ProtoError struct{ Response }

func (e *ProtoError) Error() string { return "ftp: " + e.Response.Error() }

func isNeg(code int) bool { return code >= 400 }

// client is one FTP control connection.
type client struct {
	cfg      Config
	mu       sync.Mutex
	conn     net.Conn
	text     *textproto.Conn
	feat     map[string]string
	pwd      string
	prot     bool // data channel protection active
	enc      encoding.Encoding
	welcome  string
	system   string
	lastUsed time.Time
	closed   bool
	listCmd  string
}

func (c *client) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(format, args...)
	}
}

func dial(ctx context.Context, cfg Config) (*client, error) {
	if cfg.Port == 0 {
		if cfg.TLS == "implicit" {
			cfg.Port = 990
		} else {
			cfg.Port = 21
		}
	}
	if cfg.ForcePasvIP == "" {
		cfg.ForcePasvIP = "auto"
	}
	c := &client{cfg: cfg, feat: map[string]string{}}
	switch strings.ToLower(strings.ReplaceAll(cfg.Encoding, "-", "")) {
	case "", "utf8", "auto":
	case "windows1252", "cp1252":
		c.enc = charmap.Windows1252
	case "iso88591", "latin1":
		c.enc = charmap.ISO8859_1
	case "iso885915", "latin9":
		c.enc = charmap.ISO8859_15
	case "cp850":
		c.enc = charmap.CodePage850
	case "cp437":
		c.enc = charmap.CodePage437
	case "windows1250":
		c.enc = charmap.Windows1250
	case "windows1251":
		c.enc = charmap.Windows1251
	case "koi8r":
		c.enc = charmap.KOI8R
	}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *client) addr() string { return net.JoinHostPort(c.cfg.Host, strconv.Itoa(c.cfg.Port)) }

func (c *client) connect(ctx context.Context) error {
	if c.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}
	c.logf("Connecting to %s", c.addr())
	nc, err := c.cfg.Dialer.DialContext(ctx, "tcp", c.addr())
	if err != nil {
		return err
	}
	if c.cfg.TLS == "implicit" {
		tc := tls.Client(nc, c.tlsConfig())
		if err := tc.HandshakeContext(ctx); err != nil {
			nc.Close()
			return err
		}
		nc = tc
		c.prot = true
	}
	c.conn = nc
	c.text = textproto.NewConn(nc)
	stop := context.AfterFunc(ctx, func() { nc.SetDeadline(time.Now()) })
	defer stop()

	r, err := c.readResponse()
	if err != nil {
		nc.Close()
		return err
	}
	if r.Code != 220 {
		nc.Close()
		return &ProtoError{r}
	}
	c.welcome = r.Msg
	if c.cfg.TLS == "explicit" {
		r, err := c.cmd("AUTH TLS")
		if err != nil {
			nc.Close()
			return err
		}
		if r.Code != 234 {
			if r2, err := c.cmd("AUTH SSL"); err != nil || (r2.Code != 234 && r2.Code != 334) {
				nc.Close()
				return fmt.Errorf("server does not support explicit TLS: %s", r.Msg)
			}
		}
		tc := tls.Client(nc, c.tlsConfig())
		if err := tc.HandshakeContext(ctx); err != nil {
			nc.Close()
			return err
		}
		c.conn = tc
		c.text = textproto.NewConn(tc)
	}
	if err := c.login(); err != nil {
		c.conn.Close()
		return err
	}
	return nil
}

func (c *client) tlsConfig() *tls.Config {
	if c.cfg.TLSVerifier != nil {
		return c.cfg.TLSVerifier.Config(c.cfg.Host)
	}
	return &tls.Config{ServerName: c.cfg.Host, ClientSessionCache: tls.NewLRUClientSessionCache(16)}
}

func (c *client) login() error {
	user := c.cfg.User
	if user == "" {
		user = "anonymous"
	}
	r, err := c.cmd("USER " + user)
	if err != nil {
		return err
	}
	if r.Code == 331 {
		pass := c.cfg.Password
		if pass == "" && user == "anonymous" {
			pass = "anonymous@"
		}
		r, err = c.cmdSecret("PASS "+pass, "PASS ****")
		if err != nil {
			return err
		}
	}
	if r.Code == 332 {
		r, err = c.cmd("ACCT " + c.cfg.Account)
		if err != nil {
			return err
		}
	}
	if r.Code != 230 && r.Code != 202 {
		return &ProtoError{r}
	}
	if r, err := c.cmd("SYST"); err == nil && r.Code == 215 {
		c.system = r.Msg
	}
	c.readFeatures()
	if c.cfg.TLS != "none" && c.cfg.TLS != "" {
		c.cmd("PBSZ 0")
		if r, err := c.cmd("PROT P"); err == nil && r.Code == 200 {
			c.prot = true
		} else {
			c.prot = false
		}
	}
	if _, ok := c.feat["UTF8"]; ok && c.enc == nil {
		c.cmd("OPTS UTF8 ON")
	}
	if _, err := c.cmd("TYPE I"); err != nil {
		return err
	}
	if r, err := c.cmd("PWD"); err == nil && r.Code == 257 {
		c.pwd = parsePWD(r.Msg)
		c.pwd = c.decode(c.pwd)
	}
	if c.pwd == "" {
		c.pwd = "/"
	}
	c.lastUsed = time.Now()
	return nil
}

func parsePWD(msg string) string {
	i := strings.Index(msg, `"`)
	if i < 0 {
		return strings.TrimSpace(msg)
	}
	var sb strings.Builder
	for j := i + 1; j < len(msg); j++ {
		if msg[j] == '"' {
			if j+1 < len(msg) && msg[j+1] == '"' {
				sb.WriteByte('"')
				j++
				continue
			}
			break
		}
		sb.WriteByte(msg[j])
	}
	return sb.String()
}

func (c *client) readFeatures() {
	r, err := c.cmd("FEAT")
	if err != nil || r.Code != 211 {
		return
	}
	for _, ln := range strings.Split(r.Msg, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(strings.ToLower(ln), "features") || strings.HasPrefix(strings.ToLower(ln), "end") {
			continue
		}
		parts := strings.SplitN(ln, " ", 2)
		k := strings.ToUpper(parts[0])
		v := ""
		if len(parts) > 1 {
			v = parts[1]
		}
		c.feat[k] = v
	}
}

func (c *client) has(f string) bool {
	_, ok := c.feat[f]
	return ok
}

func (c *client) encode(s string) string {
	if c.enc == nil {
		return s
	}
	if out, err := c.enc.NewEncoder().String(s); err == nil {
		return out
	}
	return s
}

func (c *client) decode(s string) string {
	if c.enc == nil {
		return s
	}
	if out, err := c.enc.NewDecoder().String(s); err == nil {
		return out
	}
	return s
}

func (c *client) readResponse() (Response, error) {
	if c.cfg.Timeout > 0 {
		c.conn.SetReadDeadline(time.Now().Add(c.cfg.Timeout))
		defer c.conn.SetReadDeadline(time.Time{})
	}
	line, err := c.text.ReadLine()
	if err != nil {
		return Response{}, err
	}
	if len(line) < 4 {
		return Response{}, fmt.Errorf("ftp: short response %q", line)
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return Response{}, fmt.Errorf("ftp: bad response %q", line)
	}
	msg := line[4:]
	if line[3] == '-' {
		var sb strings.Builder
		sb.WriteString(msg)
		end := line[:3] + " "
		for {
			l, err := c.text.ReadLine()
			if err != nil {
				return Response{}, err
			}
			if strings.HasPrefix(l, end) {
				sb.WriteString("\n" + l[4:])
				break
			}
			sb.WriteString("\n" + l)
		}
		msg = sb.String()
	}
	c.logf("< %d %s", code, firstLine(msg))
	return Response{Code: code, Msg: msg}, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func (c *client) cmd(line string) (Response, error) { return c.cmdSecret(line, line) }

func (c *client) cmdSecret(line, logLine string) (Response, error) {
	c.logf("> %s", logLine)
	if c.cfg.Timeout > 0 {
		c.conn.SetWriteDeadline(time.Now().Add(c.cfg.Timeout))
	}
	if err := c.text.PrintfLine("%s", line); err != nil {
		return Response{}, err
	}
	c.conn.SetWriteDeadline(time.Time{})
	c.lastUsed = time.Now()
	return c.readResponse()
}

// expect sends a command and requires a reply in [lo, hi].
func (c *client) expect(line string, codes ...int) (Response, error) {
	r, err := c.cmd(line)
	if err != nil {
		return r, err
	}
	for _, code := range codes {
		if r.Code == code {
			return r, nil
		}
	}
	return r, &ProtoError{r}
}

// dataConn opens a data connection and issues cmd. The returned function
// must be called after the transfer to read the final reply.
func (c *client) openData(ctx context.Context, cmd string, offset int64) (net.Conn, error) {
	if offset > 0 {
		if _, err := c.expect(fmt.Sprintf("REST %d", offset), 350); err != nil {
			return nil, err
		}
	}
	if c.cfg.Passive || c.cfg.ViaProxy {
		addr, err := c.passive()
		if err != nil {
			return nil, err
		}
		dctx := ctx
		if c.cfg.Timeout > 0 {
			var cancel context.CancelFunc
			dctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
			defer cancel()
		}
		dc, err := c.cfg.Dialer.DialContext(dctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("data connection to %s: %w", addr, err)
		}
		r, err := c.cmd(cmd)
		if err != nil {
			dc.Close()
			return nil, err
		}
		if r.Code != 150 && r.Code != 125 {
			dc.Close()
			return nil, &ProtoError{r}
		}
		return c.wrapData(dc)
	}
	// Active mode
	ln, err := c.listenActive()
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	r, err := c.cmd(cmd)
	if err != nil {
		return nil, err
	}
	if r.Code != 150 && r.Code != 125 {
		return nil, &ProtoError{r}
	}
	if tl, ok := ln.(*net.TCPListener); ok {
		to := c.cfg.Timeout
		if to == 0 {
			to = 30 * time.Second
		}
		tl.SetDeadline(time.Now().Add(to))
	}
	dc, err := ln.Accept()
	if err != nil {
		return nil, fmt.Errorf("active mode: server did not connect: %w", err)
	}
	return c.wrapData(dc)
}

func (c *client) wrapData(dc net.Conn) (net.Conn, error) {
	if !c.prot {
		return dc, nil
	}
	tc := tls.Client(dc, c.tlsConfig())
	if err := tc.Handshake(); err != nil {
		dc.Close()
		return nil, fmt.Errorf("data channel TLS: %w", err)
	}
	return tc, nil
}

func (c *client) passive() (string, error) {
	host, _, _ := net.SplitHostPort(c.addr())
	if !c.cfg.DisableEPSV {
		r, err := c.cmd("EPSV")
		if err != nil {
			return "", err
		}
		if r.Code == 229 {
			i, j := strings.Index(r.Msg, "("), strings.LastIndex(r.Msg, ")")
			if i >= 0 && j > i {
				parts := strings.Split(r.Msg[i+1:j], string(r.Msg[i+1]))
				if len(parts) >= 4 {
					return net.JoinHostPort(host, parts[3]), nil
				}
			}
		}
		c.cfg.DisableEPSV = true
	}
	r, err := c.expect("PASV", 227)
	if err != nil {
		return "", err
	}
	i := strings.IndexAny(r.Msg, "0123456789")
	if i < 0 {
		return "", fmt.Errorf("ftp: cannot parse PASV reply %q", r.Msg)
	}
	j := i
	for j < len(r.Msg) && (r.Msg[j] >= '0' && r.Msg[j] <= '9' || r.Msg[j] == ',') {
		j++
	}
	nums := strings.Split(r.Msg[i:j], ",")
	if len(nums) != 6 {
		return "", fmt.Errorf("ftp: cannot parse PASV reply %q", r.Msg)
	}
	p1, _ := strconv.Atoi(nums[4])
	p2, _ := strconv.Atoi(nums[5])
	port := strconv.Itoa(p1*256 + p2)
	ip := strings.Join(nums[:4], ".")
	use := ip
	switch c.cfg.ForcePasvIP {
	case "on":
		use = host
	case "auto":
		pip := net.ParseIP(ip)
		if c.cfg.ViaProxy || pip == nil || pip.IsPrivate() || pip.IsLoopback() || pip.IsUnspecified() {
			if hip := net.ParseIP(host); hip == nil || !(hip.IsPrivate() || hip.IsLoopback()) || pip == nil || pip.IsUnspecified() {
				use = host
			}
			if c.cfg.ViaProxy {
				use = host
			}
		}
	}
	return net.JoinHostPort(use, port), nil
}

func (c *client) listenActive() (net.Listener, error) {
	la, _ := c.conn.LocalAddr().(*net.TCPAddr)
	if la == nil {
		if tc, ok := c.conn.(*tls.Conn); ok {
			la, _ = tc.NetConn().LocalAddr().(*net.TCPAddr)
		}
	}
	if la == nil {
		return nil, errors.New("active mode is not available through a proxy or tunnel")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(la.IP.String(), "0"))
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ip := la.IP
	if c.cfg.ActiveIP != "" {
		ip = net.ParseIP(c.cfg.ActiveIP)
	}
	if ip4 := ip.To4(); ip4 != nil {
		cmd := fmt.Sprintf("PORT %d,%d,%d,%d,%d,%d", ip4[0], ip4[1], ip4[2], ip4[3], port/256, port%256)
		if _, err := c.expect(cmd, 200); err != nil {
			ln.Close()
			return nil, err
		}
	} else {
		cmd := fmt.Sprintf("EPRT |2|%s|%d|", ip.String(), port)
		if _, err := c.expect(cmd, 200); err != nil {
			ln.Close()
			return nil, err
		}
	}
	return ln, nil
}

// finish reads the final reply of a data transfer.
func (c *client) finish() error {
	r, err := c.readResponse()
	if err != nil {
		return err
	}
	if r.Code != 226 && r.Code != 250 && r.Code != 200 {
		return &ProtoError{r}
	}
	return nil
}

// readLines runs a listing command and returns its lines.
func (c *client) readLines(ctx context.Context, cmd string) ([]string, error) {
	dc, err := c.openData(ctx, cmd, 0)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { dc.SetDeadline(time.Now()) })
	var lines []string
	sc := bufio.NewScanner(dc)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	stop()
	serr := sc.Err()
	dc.Close()
	ferr := c.finish()
	if serr != nil {
		return nil, serr
	}
	return lines, ferr
}

func (c *client) quit() {
	if c.closed {
		return
	}
	c.closed = true
	c.conn.SetDeadline(time.Now().Add(2 * time.Second))
	c.text.PrintfLine("QUIT")
	c.text.ReadLine()
	c.conn.Close()
}

// dataStream wraps a transfer data connection; Close finishes the transfer
// and releases the control connection lock.
type dataStream struct {
	net.Conn
	c         *client
	unlock    func()
	once      sync.Once
	err       error
	abortRead bool
}

func (d *dataStream) Close() error {
	d.once.Do(func() {
		if tc, ok := d.Conn.(*tls.Conn); ok && !d.abortRead {
			tc.CloseWrite()
		}
		d.Conn.Close()
		if d.abortRead {
			// Download closed early: abort and drain every reply. The
			// server may already have sent the RETR completion (226) before
			// the ABOR reply (225/226), or a 426 before it: an unread reply
			// would shift all the following ones.
			d.c.conn.SetDeadline(time.Now().Add(10 * time.Second))
			d.c.text.PrintfLine("ABOR")
			r, err := d.c.readResponse()
			if err == nil {
				if r.Code >= 400 {
					d.c.readResponse() // 426/451 then the ABOR reply
				} else {
					// maybe the RETR completion: wait briefly for the ABOR reply
					d.c.conn.SetDeadline(time.Now().Add(time.Second))
					d.c.readResponse()
				}
			}
			d.c.conn.SetDeadline(time.Time{})
		} else {
			d.err = d.c.finish()
		}
		d.unlock()
	})
	return d.err
}

type readStream struct {
	*dataStream
	eof bool
}

func (r *readStream) Read(p []byte) (int, error) {
	n, err := r.dataStream.Conn.Read(p)
	if err == io.EOF {
		r.eof = true
	}
	return n, err
}

func (r *readStream) Close() error {
	if !r.eof {
		// The caller may have read exactly the file size without seeing
		// EOF: check briefly whether the data is exhausted before aborting.
		r.dataStream.Conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var buf [4096]byte
		for {
			_, err := r.dataStream.Conn.Read(buf[:])
			if err == io.EOF {
				r.eof = true
				break
			}
			if err != nil {
				break
			}
		}
		r.dataStream.Conn.SetReadDeadline(time.Time{})
	}
	if !r.eof {
		r.abortRead = true
	}
	return r.dataStream.Close()
}

// WriteTo makes io.Copy use the raw connection.
func (r *readStream) WriteTo(w io.Writer) (int64, error) {
	n, err := io.Copy(w, struct{ io.Reader }{r.dataStream.Conn})
	if err == nil {
		r.eof = true
	}
	return n, err
}
