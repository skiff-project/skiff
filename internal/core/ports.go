package core

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// The ports a server listens on, read with ss, netstat or /proc/net/tcp
// over the connection. A web page served on one of them opens in the
// browser of this computer through a tunnel made on the spot.

// PortInfo is a TCP port a server listens on.
type PortInfo struct {
	Port      int      `json:"port"`
	Addresses []string `json:"addresses"` // 0.0.0.0, ::, 127.0.0.1, ::1, 10.0.0.5…
	// Scope: public (every address), local (the server only), interface
	// (one address of the server).
	Scope   string `json:"scope"`
	Process string `json:"process,omitempty"` // nginx (shown to root, or for the user's own processes)
	PID     int    `json:"pid,omitempty"`
	Service string `json:"service,omitempty"` // the usual service of the port
}

// knownPorts names the usual services.
var knownPorts = map[int]string{
	21: "FTP", 22: "SSH", 25: "SMTP", 53: "DNS", 80: "HTTP", 110: "POP3", 143: "IMAP", 443: "HTTPS",
	465: "SMTPS", 587: "SMTP", 993: "IMAPS", 995: "POP3S", 1433: "SQL Server", 2375: "Docker", 2376: "Docker",
	3000: "HTTP", 3306: "MySQL / MariaDB", 5000: "HTTP", 5173: "Vite", 5432: "PostgreSQL", 5672: "RabbitMQ",
	6379: "Redis", 8000: "HTTP", 8080: "HTTP", 8443: "HTTPS", 8888: "HTTP", 9000: "PHP-FPM", 9090: "Prometheus",
	9200: "Elasticsearch", 11211: "Memcached", 15672: "RabbitMQ", 27017: "MongoDB",
}

// ListeningPorts lists the TCP ports the server of a connection listens on.
func (c *Core) ListeningPorts(ctx context.Context, conn string) ([]PortInfo, error) {
	cn, ok := c.hasShell(conn)
	if !ok {
		return nil, errors.New("the ports need an SFTP or SCP connection with a shell")
	}
	cmd := `if command -v ss >/dev/null 2>&1; then echo SKIFF:SS; ss -Htlnp 2>/dev/null || ss -tlnp 2>/dev/null
elif command -v netstat >/dev/null 2>&1; then echo SKIFF:NETSTAT; netstat -tlnp 2>/dev/null || netstat -tln 2>/dev/null
else echo SKIFF:PROC; cat /proc/net/tcp /proc/net/tcp6 2>/dev/null; fi; true`
	out, err := cn.sshOutput(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return parsePorts(out), nil
}

type listenLine struct {
	addr    string
	port    int
	process string
	pid     int
}

func parsePorts(out string) []PortInfo {
	var lines []listenLine
	switch {
	case strings.Contains(out, "SKIFF:SS"):
		lines = parseSS(out)
	case strings.Contains(out, "SKIFF:NETSTAT"):
		lines = parseNetstat(out)
	default:
		lines = parseProcNet(out)
	}
	// one row per port and process, with all its addresses
	type key struct {
		port int
		proc string
	}
	by := map[key]*PortInfo{}
	var order []key
	for _, l := range lines {
		k := key{l.port, l.process}
		p, ok := by[k]
		if !ok {
			p = &PortInfo{Port: l.port, Process: l.process, PID: l.pid, Service: knownPorts[l.port]}
			by[k] = p
			order = append(order, k)
		}
		if !contains(p.Addresses, l.addr) {
			p.Addresses = append(p.Addresses, l.addr)
		}
	}
	res := make([]PortInfo, 0, len(order))
	for _, k := range order {
		p := by[k]
		p.Scope = scopeOf(p.Addresses)
		res = append(res, *p)
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Port < res[j].Port })
	return res
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func scopeOf(addrs []string) string {
	scope := "local"
	for _, a := range addrs {
		switch {
		case a == "0.0.0.0" || a == "::" || a == "*":
			return "public"
		case strings.HasPrefix(a, "127.") || a == "::1" || a == "localhost":
		default:
			scope = "interface"
		}
	}
	return scope
}

// splitAddr cuts "0.0.0.0:80", "[::]:80", "*:22", "127.0.0.53%lo:53".
func splitAddr(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return "", 0, false
	}
	host := strings.Trim(s[:i], "[]")
	if j := strings.IndexByte(host, '%'); j >= 0 {
		host = host[:j]
	}
	return host, port, true
}

// ss -Htlnp: LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=812,fd=6),…)
func parseSS(out string) []listenLine {
	var res []listenLine
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 || f[0] != "LISTEN" {
			continue
		}
		host, port, ok := splitAddr(f[3])
		if !ok {
			continue
		}
		l := listenLine{addr: host, port: port}
		if len(f) > 5 {
			rest := strings.Join(f[5:], " ")
			if i := strings.Index(rest, `(("`); i >= 0 {
				name := rest[i+3:]
				if j := strings.IndexByte(name, '"'); j >= 0 {
					l.process = name[:j]
				}
			}
			if i := strings.Index(rest, "pid="); i >= 0 {
				n := rest[i+4:]
				if j := strings.IndexAny(n, ",)"); j >= 0 {
					l.pid, _ = strconv.Atoi(n[:j])
				}
			}
		}
		res = append(res, l)
	}
	return res
}

// netstat -tlnp: tcp 0 0 0.0.0.0:22 0.0.0.0:* LISTEN 123/sshd
func parseNetstat(out string) []listenLine {
	var res []listenLine
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 6 || !strings.HasPrefix(f[0], "tcp") || f[5] != "LISTEN" {
			continue
		}
		host, port, ok := splitAddr(f[3])
		if !ok {
			continue
		}
		l := listenLine{addr: host, port: port}
		if len(f) > 6 && f[6] != "-" {
			pid, name, _ := strings.Cut(f[6], "/")
			l.pid, _ = strconv.Atoi(pid)
			l.process = strings.TrimRight(strings.Fields(name + " ")[0], ":")
		}
		res = append(res, l)
	}
	return res
}

// /proc/net/tcp: "  0: 0100007F:0BB8 00000000:0000 0A …" (0A = listening)
func parseProcNet(out string) []listenLine {
	var res []listenLine
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		hexAddr, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil {
			continue
		}
		ip := procIP(hexAddr)
		if ip == "" {
			continue
		}
		res = append(res, listenLine{addr: ip, port: int(port)})
	}
	return res
}

// procIP decodes an address of /proc/net/tcp (32-bit words, little-endian).
func procIP(h string) string {
	if len(h) != 8 && len(h) != 32 {
		return ""
	}
	b := make([]byte, len(h)/2)
	for i := 0; i < len(b); i++ {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return ""
		}
		b[i] = byte(v)
	}
	for w := 0; w+4 <= len(b); w += 4 {
		b[w], b[w+1], b[w+2], b[w+3] = b[w+3], b[w+2], b[w+1], b[w]
	}
	ip := net.IP(b)
	if ip.IsUnspecified() {
		if len(b) == 4 {
			return "0.0.0.0"
		}
		return "::"
	}
	return ip.String()
}

// OpenURL opens a web address in the browser of this computer (the
// SKIFF_BROWSER command when it is set).
func OpenURL(u string) error {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return errors.New("only http and https addresses can be opened")
	}
	if b := os.Getenv("SKIFF_BROWSER"); b != "" {
		return exec.Command(b, u).Start()
	}
	return OpenWithDefault(u)
}
