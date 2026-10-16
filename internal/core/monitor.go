package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"skiff/internal/vfs"
)

// The server monitor samples a Linux server through /proc with a single
// command per sample, so that nothing has to be installed on the server.
// CPU and network rates are differences between two samples.

type MonitorDisk struct {
	Mount string `json:"mount"`
	FS    string `json:"fs"`
	Total int64  `json:"total"`
	Used  int64  `json:"used"`
}

type MonitorProc struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	Name    string  `json:"name"`
	State   string  `json:"state"`
	CPU     float64 `json:"cpu"` // % of one CPU since the previous sample
	Mem     int64   `json:"mem"` // resident memory, bytes
	Threads int     `json:"threads"`
}

type MonitorSample struct {
	Time      time.Time     `json:"time"`
	Hostname  string        `json:"hostname"`
	Kernel    string        `json:"kernel"`
	CPUs      int           `json:"cpus"`
	Uptime    float64       `json:"uptime"` // seconds
	Load      [3]float64    `json:"load"`
	CPU       float64       `json:"cpu"` // % busy since the previous sample, -1 on the first one
	MemTotal  int64         `json:"memTotal"`
	MemUsed   int64         `json:"memUsed"`
	SwapTotal int64         `json:"swapTotal"`
	SwapUsed  int64         `json:"swapUsed"`
	NetRx     float64       `json:"netRx"` // bytes/s, -1 on the first sample
	NetTx     float64       `json:"netTx"`
	Disks     []MonitorDisk `json:"disks"`
	Procs     []MonitorProc `json:"procs"`
	ProcCount int           `json:"procCount"`
}

const monitorScript = `LC_ALL=C; export LC_ALL
[ -r /proc/stat ] || { echo "no /proc/stat" >&2; exit 3; }
echo @@uname; uname -n; uname -sr
echo @@nproc; nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo
echo @@uptime; cat /proc/uptime
echo @@loadavg; cat /proc/loadavg
echo @@stat; head -n 1 /proc/stat
echo @@meminfo; cat /proc/meminfo
echo @@df; df -Pk 2>/dev/null
echo @@net; cat /proc/net/dev
echo @@page; getconf PAGESIZE 2>/dev/null || echo 4096
echo @@procs; cat /proc/[0-9]*/stat 2>/dev/null
echo @@owners; ls -ldn /proc/[0-9]* 2>/dev/null
`

// monitorPrev is what a sample keeps for the next one.
type monitorPrev struct {
	at          time.Time
	busy, total uint64
	ticks       map[int]uint64
	rx, tx      uint64
}

type monitorState struct {
	mu    sync.Mutex
	prev  map[string]*monitorPrev
	users map[string]map[int]string // conn -> uid -> name
}

// MonitorSample takes a sample of the server behind an SSH connection. key
// identifies the viewer (each keeps its own previous sample).
func (c *Core) MonitorSample(ctx context.Context, conn, key string) (MonitorSample, error) {
	cn, err := c.Conn(conn)
	if err != nil {
		return MonitorSample{}, err
	}
	out, err := cn.sshOutput(ctx, monitorScript)
	if err != nil {
		if strings.Contains(err.Error(), "no /proc/stat") || strings.Contains(err.Error(), "status 3") {
			return MonitorSample{}, errors.New("the monitor needs a Linux server (/proc not found)")
		}
		return MonitorSample{}, err
	}
	m := &c.monitor
	m.mu.Lock()
	if m.prev == nil {
		m.prev, m.users = map[string]*monitorPrev{}, map[string]map[int]string{}
	}
	users := m.users[conn]
	m.mu.Unlock()
	if users == nil {
		if pw, err := cn.sshOutput(ctx, "cat /etc/passwd"); err == nil {
			users = parsePasswd(pw)
			m.mu.Lock()
			m.users[conn] = users
			m.mu.Unlock()
		}
	}
	s, cur := parseMonitor(out, users)
	m.mu.Lock()
	id := conn + "|" + key
	prev := m.prev[id]
	m.prev[id] = cur
	for k, p := range m.prev { // forget the viewers gone for a while
		if time.Since(p.at) > 2*time.Minute {
			delete(m.prev, k)
		}
	}
	m.mu.Unlock()
	applyRates(&s, cur, prev)
	return s, nil
}

// MonitorKill sends a signal to a process of the server.
func (c *Core) MonitorKill(ctx context.Context, conn string, pid int, signal string) error {
	switch signal {
	case "TERM", "KILL", "HUP", "INT", "STOP", "CONT":
	default:
		return fmt.Errorf("unsupported signal %q", signal)
	}
	cn, err := c.Conn(conn)
	if err != nil {
		return err
	}
	_, err = cn.sshOutput(ctx, fmt.Sprintf("kill -s %s %d", signal, pid))
	return err
}

// sshOutput runs a command on the connection's server (or in its
// container) and returns its standard output; the error carries the
// standard error.
func (cn *Conn) sshOutput(ctx context.Context, cmd string) (string, error) {
	var stdout bytes.Buffer
	err := cn.execStream(ctx, cmd, &stdout)
	return stdout.String(), err
}

// execStream runs a command on the connection's server (or in its
// container), writing its standard output to stdout as it comes; the
// error carries the standard error. Cancelling ctx stops the command.
func (cn *Conn) execStream(ctx context.Context, cmd string, stdout io.Writer) error {
	s := cn.SSH()
	var stderr bytes.Buffer
	if s == nil {
		cn.mu.Lock()
		fs, isContainer := cn.fs, cn.container != nil
		cn.mu.Unlock()
		ex, ok := fs.(vfs.Executor)
		if !isContainer || !ok {
			return errors.New("only available for SFTP, SCP and container connections")
		}
		code, err := ex.Exec(ctx, cmd, nil, stdout, &stderr)
		if err == nil && code != 0 {
			err = fmt.Errorf("%s (exit status %d)", strings.TrimSpace(stderr.String()), code)
		}
		return err
	}
	sess, err := s.NewSession()
	if err != nil {
		cn.closeIdle() // the server limits sessions per connection
		if sess, err = s.NewSession(); err != nil {
			return err
		}
	}
	defer sess.Close()
	sess.Stdout, sess.Stderr = stdout, &stderr
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case err = <-done:
	case <-ctx.Done():
		sess.Close()
		return ctx.Err()
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s (%v)", msg, err)
		}
		return err
	}
	return nil
}

func parsePasswd(s string) map[int]string {
	out := map[int]string{}
	for _, ln := range strings.Split(s, "\n") {
		f := strings.Split(ln, ":")
		if len(f) > 2 {
			if uid, err := strconv.Atoi(f[2]); err == nil {
				if _, seen := out[uid]; !seen {
					out[uid] = f[0]
				}
			}
		}
	}
	return out
}

func sections(out string) map[string][]string {
	sec := map[string][]string{}
	cur := ""
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		ln := sc.Text()
		if strings.HasPrefix(ln, "@@") {
			cur = ln[2:]
			continue
		}
		sec[cur] = append(sec[cur], ln)
	}
	return sec
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

var pseudoFS = regexp.MustCompile(`^(tmpfs|devtmpfs|udev|none|shm|proc|sysfs|cgroup2?|squashfs|efivarfs|ramfs|nsfs|tracefs)$`)

func parseMonitor(out string, users map[int]string) (MonitorSample, *monitorPrev) {
	sec := sections(out)
	s := MonitorSample{Time: time.Now(), CPU: -1, NetRx: -1, NetTx: -1, Disks: []MonitorDisk{}, Procs: []MonitorProc{}}
	cur := &monitorPrev{at: s.Time, ticks: map[int]uint64{}}
	if u := sec["uname"]; len(u) >= 2 {
		s.Hostname, s.Kernel = strings.TrimSpace(u[0]), strings.TrimSpace(u[1])
	}
	if n := sec["nproc"]; len(n) > 0 {
		s.CPUs = int(atoi64(n[0]))
	}
	if u := sec["uptime"]; len(u) > 0 {
		if f := strings.Fields(u[0]); len(f) > 0 {
			s.Uptime, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if l := sec["loadavg"]; len(l) > 0 {
		f := strings.Fields(l[0])
		for i := 0; i < 3 && i < len(f); i++ {
			s.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if st := sec["stat"]; len(st) > 0 {
		f := strings.Fields(st[0])
		for i, v := range f[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			if i >= 8 { // guest time is already counted in user time
				break
			}
			cur.total += n
			if i != 3 && i != 4 { // idle, iowait
				cur.busy += n
			}
		}
	}
	mem := map[string]int64{}
	for _, ln := range sec["meminfo"] {
		if k, v, ok := strings.Cut(ln, ":"); ok {
			if f := strings.Fields(v); len(f) > 0 {
				mem[k] = atoi64(f[0]) * 1024
			}
		}
	}
	s.MemTotal = mem["MemTotal"]
	avail, ok := mem["MemAvailable"]
	if !ok {
		avail = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	s.MemUsed = s.MemTotal - avail
	s.SwapTotal, s.SwapUsed = mem["SwapTotal"], mem["SwapTotal"]-mem["SwapFree"]
	seen := map[string]bool{}
	for i, ln := range sec["df"] {
		f := strings.Fields(ln)
		if i == 0 || len(f) < 6 || pseudoFS.MatchString(f[0]) {
			continue
		}
		mount := strings.Join(f[5:], " ")
		if strings.HasPrefix(mount, "/proc") || strings.HasPrefix(mount, "/sys") || strings.HasPrefix(mount, "/dev") || strings.HasPrefix(mount, "/run") || strings.HasPrefix(mount, "/snap/") {
			continue
		}
		total, used := atoi64(f[1])*1024, atoi64(f[2])*1024
		// bind mounts show the same file system again
		id := strconv.FormatInt(total, 10) + "|" + strconv.FormatInt(used, 10)
		if total == 0 || seen[id] {
			continue
		}
		seen[id] = true
		s.Disks = append(s.Disks, MonitorDisk{Mount: mount, FS: f[0], Total: total, Used: used})
	}
	for _, ln := range sec["net"] {
		name, rest, ok := strings.Cut(ln, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		if f := strings.Fields(rest); len(f) >= 9 {
			rx, _ := strconv.ParseUint(f[0], 10, 64)
			tx, _ := strconv.ParseUint(f[8], 10, 64)
			cur.rx += rx
			cur.tx += tx
		}
	}
	page := int64(4096)
	if p := sec["page"]; len(p) > 0 && atoi64(p[0]) > 0 {
		page = atoi64(p[0])
	}
	owners := map[int]int{}
	for _, ln := range sec["owners"] {
		f := strings.Fields(ln)
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(f[len(f)-1], "/proc/"))
		uid, err2 := strconv.Atoi(f[2])
		if err == nil && err2 == nil {
			owners[pid] = uid
		}
	}
	for _, ln := range sec["procs"] {
		open, close := strings.IndexByte(ln, '('), strings.LastIndexByte(ln, ')')
		if open < 0 || close < open {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(ln[:open]))
		if err != nil {
			continue
		}
		f := strings.Fields(ln[close+1:]) // f[0] is field 3 (state)
		if len(f) < 22 {
			continue
		}
		ut, _ := strconv.ParseUint(f[11], 10, 64)
		st, _ := strconv.ParseUint(f[12], 10, 64)
		cur.ticks[pid] = ut + st
		p := MonitorProc{PID: pid, Name: ln[open+1 : close], State: f[0], Threads: int(atoi64(f[17])), Mem: atoi64(f[21]) * page}
		if uid, ok := owners[pid]; ok {
			if name := users[uid]; name != "" {
				p.User = name
			} else {
				p.User = strconv.Itoa(uid)
			}
		}
		s.Procs = append(s.Procs, p)
	}
	s.ProcCount = len(s.Procs)
	return s, cur
}

// applyRates fills the CPU and network rates from the previous sample and
// keeps the busiest processes.
func applyRates(s *MonitorSample, cur, prev *monitorPrev) {
	const hz = 100 // USER_HZ on every Linux platform
	if prev != nil {
		dt := cur.at.Sub(prev.at).Seconds()
		if cur.total > prev.total {
			s.CPU = float64(cur.busy-prev.busy) / float64(cur.total-prev.total) * 100
		}
		if dt > 0 {
			if cur.rx >= prev.rx && cur.tx >= prev.tx {
				s.NetRx, s.NetTx = float64(cur.rx-prev.rx)/dt, float64(cur.tx-prev.tx)/dt
			}
			for i := range s.Procs {
				p := &s.Procs[i]
				if before, ok := prev.ticks[p.PID]; ok && cur.ticks[p.PID] >= before {
					p.CPU = float64(cur.ticks[p.PID]-before) / hz / dt * 100
				}
			}
		}
	}
	sort.Slice(s.Procs, func(a, b int) bool {
		if s.Procs[a].CPU != s.Procs[b].CPU {
			return s.Procs[a].CPU > s.Procs[b].CPU
		}
		return s.Procs[a].Mem > s.Procs[b].Mem
	})
	if len(s.Procs) > 40 {
		s.Procs = s.Procs[:40]
	}
}
