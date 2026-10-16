package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"skiff/internal/config"
)

// TunnelInfo is the UI view of a port forward.
type TunnelInfo struct {
	ID        string    `json:"id"`
	Conn      string    `json:"conn"`
	ConnTitle string    `json:"connTitle"`
	Type      string    `json:"type"`
	Bind      string    `json:"bind"`
	Dest      string    `json:"dest,omitempty"`
	Label     string    `json:"label,omitempty"`
	Active    int64     `json:"active"`
	Total     int64     `json:"total"`
	BytesIn   int64     `json:"bytesIn"`
	BytesOut  int64     `json:"bytesOut"`
	Started   time.Time `json:"started"`
	Error     string    `json:"error,omitempty"`
	Running   bool      `json:"running"`
}

type tunnel struct {
	info    TunnelInfo
	ln      net.Listener
	active  atomic.Int64
	total   atomic.Int64
	in, out atomic.Int64
	mu      sync.Mutex
	conns   map[net.Conn]bool
	closed  bool
}

type tunnels struct {
	core *Core
	mu   sync.Mutex
	m    map[string]*tunnel
}

func newTunnels(c *Core) *tunnels {
	ts := &tunnels{core: c, m: map[string]*tunnel{}}
	go ts.reporter()
	return ts
}

func (ts *tunnels) reporter() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := map[string]int64{}
	for range t.C {
		ts.mu.Lock()
		var changed []TunnelInfo
		for id, tu := range ts.m {
			sig := tu.in.Load() + tu.out.Load()*3 + tu.active.Load()*7
			if last[id] != sig {
				last[id] = sig
				changed = append(changed, tu.snapshot())
			}
		}
		ts.mu.Unlock()
		for _, c := range changed {
			ts.core.Bus.Publish("tunnel.update", c)
		}
	}
}

func (t *tunnel) snapshot() TunnelInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.info
	i.Active, i.Total, i.BytesIn, i.BytesOut = t.active.Load(), t.total.Load(), t.in.Load(), t.out.Load()
	i.Running = !t.closed
	return i
}

func (t *tunnel) track(c net.Conn, add bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if add {
		t.conns[c] = true
	} else {
		delete(t.conns, c)
	}
}

type countConn struct {
	net.Conn
	n *atomic.Int64
}

func (c countConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.n.Add(int64(n))
	return n, err
}

// pipe copies in both directions; in counts bytes towards the client.
func (t *tunnel) pipe(a, b net.Conn) {
	t.active.Add(1)
	t.total.Add(1)
	t.track(a, true)
	t.track(b, true)
	defer func() {
		a.Close()
		b.Close()
		t.track(a, false)
		t.track(b, false)
		t.active.Add(-1)
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		io.Copy(countConn{b, &t.out}, a)
		if cw, ok := b.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
		wg.Done()
	}()
	go func() {
		io.Copy(countConn{a, &t.in}, b)
		if cw, ok := a.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
		wg.Done()
	}()
	wg.Wait()
}

// StartTunnel starts a port forward on an SSH connection.
func (c *Core) StartTunnel(connID string, f config.Forward) (TunnelInfo, error) {
	cn, err := c.Conn(connID)
	if err != nil {
		return TunnelInfo{}, err
	}
	s := cn.SSH()
	if s == nil {
		return TunnelInfo{}, errors.New("tunnels require an SSH (SFTP/SCP) connection")
	}
	bindHost := f.BindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	bind := net.JoinHostPort(bindHost, strconv.Itoa(f.BindPort))
	dest := ""
	if f.DestHost != "" {
		dest = net.JoinHostPort(f.DestHost, strconv.Itoa(f.DestPort))
	}
	t := &tunnel{conns: map[net.Conn]bool{}}
	t.info = TunnelInfo{ID: randID(5), Conn: connID, ConnTitle: cn.Title, Type: f.Type, Bind: bind, Dest: dest, Label: f.Label, Started: time.Now()}
	switch f.Type {
	case "local", "dynamic":
		ln, err := net.Listen("tcp", bind)
		if err != nil {
			return TunnelInfo{}, err
		}
		t.ln = ln
		t.info.Bind = ln.Addr().String()
		go func() {
			for {
				lc, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					target := dest
					var socksVersion byte
					if f.Type == "dynamic" {
						tg, v, err := socksHandshake(lc)
						if err != nil {
							lc.Close()
							return
						}
						target, socksVersion = tg, v
					}
					rc, err := s.Client.Dial("tcp", target)
					if err != nil {
						if f.Type == "dynamic" {
							socksReply(lc, socksVersion, false)
						}
						cn.log.Warnf("Tunnel %s → %s: %v", t.info.Bind, target, err)
						lc.Close()
						return
					}
					if f.Type == "dynamic" {
						socksReply(lc, socksVersion, true)
					}
					t.pipe(lc, rc)
				}()
			}
		}()
	case "remote":
		ln, err := s.Client.Listen("tcp", bind)
		if err != nil {
			return TunnelInfo{}, fmt.Errorf("server refused to listen on %s: %w", bind, err)
		}
		t.ln = ln
		go func() {
			for {
				rc, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					lc, err := net.DialTimeout("tcp", dest, 10*time.Second)
					if err != nil {
						cn.log.Warnf("Remote tunnel → %s: %v", dest, err)
						rc.Close()
						return
					}
					t.pipe(rc, lc)
				}()
			}
		}()
	default:
		return TunnelInfo{}, fmt.Errorf("unknown tunnel type %q", f.Type)
	}
	c.tunnels.mu.Lock()
	c.tunnels.m[t.info.ID] = t
	c.tunnels.mu.Unlock()
	cn.log.Logf("Tunnel started: %s %s → %s", f.Type, t.info.Bind, dest)
	info := t.snapshot()
	c.Bus.Publish("tunnel.update", info)
	return info, nil
}

// socksHandshake implements a minimal SOCKS4/4a/5 server (no auth) and
// returns the requested destination and the protocol version (4 or 5). The
// caller answers with socksReply once the destination is reached.
func socksHandshake(c net.Conn) (string, byte, error) {
	c.SetDeadline(time.Now().Add(15 * time.Second))
	defer c.SetDeadline(time.Time{})
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return "", 0, err
	}
	if hdr[0] == 4 {
		rest := make([]byte, 6)
		if _, err := io.ReadFull(c, rest); err != nil {
			return "", 0, err
		}
		port := binary.BigEndian.Uint16(rest[0:2])
		ip := net.IP(rest[2:6])
		readZ := func() (string, error) {
			var b []byte
			one := make([]byte, 1)
			for len(b) < 256 {
				if _, err := io.ReadFull(c, one); err != nil {
					return "", err
				}
				if one[0] == 0 {
					break
				}
				b = append(b, one[0])
			}
			return string(b), nil
		}
		if _, err := readZ(); err != nil {
			return "", 0, err
		}
		host := ip.String()
		if ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] != 0 {
			h, err := readZ()
			if err != nil {
				return "", 0, err
			}
			host = h
		}
		return net.JoinHostPort(host, strconv.Itoa(int(port))), 4, nil
	}
	if hdr[0] != 5 {
		return "", 0, errors.New("unsupported SOCKS version")
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return "", 0, err
	}
	c.Write([]byte{5, 0})
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil {
		return "", 0, err
	}
	if req[1] != 1 {
		c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", 0, errors.New("only CONNECT is supported")
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return "", 0, err
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(c, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	default:
		return "", 0, errors.New("bad address type")
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return "", 0, err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb)))), 5, nil
}

// socksReply tells a SOCKS client whether the destination was reached.
func socksReply(c net.Conn, version byte, ok bool) {
	switch {
	case version == 4 && ok:
		c.Write([]byte{0, 0x5a, 0, 0, 0, 0, 0, 0})
	case version == 4:
		c.Write([]byte{0, 0x5b, 0, 0, 0, 0, 0, 0})
	case ok:
		c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	default:
		c.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0}) // host unreachable
	}
}

func (c *Core) StopTunnel(id string) {
	c.tunnels.mu.Lock()
	t, ok := c.tunnels.m[id]
	delete(c.tunnels.m, id)
	c.tunnels.mu.Unlock()
	if !ok {
		return
	}
	t.mu.Lock()
	t.closed = true
	conns := make([]net.Conn, 0, len(t.conns))
	for x := range t.conns {
		conns = append(conns, x)
	}
	t.mu.Unlock()
	if t.ln != nil {
		t.ln.Close()
	}
	for _, x := range conns {
		x.Close()
	}
	c.Bus.Publish("tunnel.closed", id)
}

func (c *Core) Tunnels() []TunnelInfo {
	c.tunnels.mu.Lock()
	defer c.tunnels.mu.Unlock()
	out := make([]TunnelInfo, 0, len(c.tunnels.m))
	for _, t := range c.tunnels.m {
		out = append(out, t.snapshot())
	}
	return out
}

func (ts *tunnels) closeConn(conn string) {
	ts.mu.Lock()
	var ids []string
	for id, t := range ts.m {
		if t.info.Conn == conn {
			ids = append(ids, id)
		}
	}
	ts.mu.Unlock()
	for _, id := range ids {
		ts.core.StopTunnel(id)
	}
}

var _ = context.Background
