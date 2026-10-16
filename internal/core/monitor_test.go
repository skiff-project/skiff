package core

import (
	"testing"
	"time"
)

const monitorOut = `@@uname
web01
Linux 6.8.0-45-generic
@@nproc
4
@@uptime
86500.25 300000.00
@@loadavg
0.52 0.40 0.31 2/180 9999
@@stat
cpu  1000 0 500 8000 500 0 0 0 0 0
@@meminfo
MemTotal:        8000000 kB
MemFree:          500000 kB
MemAvailable:    6000000 kB
SwapTotal:       2000000 kB
SwapFree:        1500000 kB
@@df
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/sda1         10000000   4000000   6000000      40% /
tmpfs               100000         0    100000       0% /run
/dev/sdb1         20000000   1000000  19000000       5% /srv/My Data
overlay           10000000   4000000   6000000      40% /var/lib/docker/overlay2/x/merged
@@net
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 999999 10 0 0 0 0 0 0 999999 10 0 0 0 0 0 0
  eth0: 1000000 100 0 0 0 0 0 0 2000000 200 0 0 0 0 0 0
@@page
4096
@@procs
1 (systemd) S 0 1 1 0 -1 4194560 1209 3868 0 1 200 100 1 3 20 0 1 0 100 45875 2560 18446744073709551615
842 (my (odd) proc) R 1 842 842 0 -1 4194560 1209 3868 0 1 50 50 1 3 20 0 3 0 100 45875 1024 18446744073709551615
@@owners
dr-xr-xr-x 9 0 0 0 Oct 16 08:50 /proc/1
dr-xr-xr-x 9 1000 1000 0 Oct 16 08:50 /proc/842
`

func TestParseMonitor(t *testing.T) {
	s, cur := parseMonitor(monitorOut, map[int]string{0: "root"})
	if s.Hostname != "web01" || s.CPUs != 4 || s.Uptime != 86500.25 || s.Load[0] != 0.52 {
		t.Errorf("header %+v", s)
	}
	if s.MemTotal != 8000000*1024 || s.MemUsed != 2000000*1024 || s.SwapUsed != 500000*1024 {
		t.Errorf("memory %d %d %d", s.MemTotal, s.MemUsed, s.SwapUsed)
	}
	// the overlay is the root file system seen again
	if len(s.Disks) != 2 || s.Disks[0].Mount != "/" || s.Disks[1].Mount != "/srv/My Data" || s.Disks[0].Used != 4000000*1024 {
		t.Errorf("disks %+v", s.Disks)
	}
	if s.ProcCount != 2 || s.Procs[1].Name != "my (odd) proc" || s.Procs[1].User != "1000" || s.Procs[0].User != "root" || s.Procs[1].Threads != 3 || s.Procs[1].Mem != 1024*4096 {
		t.Errorf("procs %+v", s.Procs)
	}
	if s.CPU != -1 || s.NetRx != -1 || cur.rx != 1000000 || cur.tx != 2000000 || cur.total != 10000 || cur.busy != 1500 {
		t.Errorf("first sample rates %v %v / %+v", s.CPU, s.NetRx, cur)
	}
	// a second sample one second later
	prev := cur
	prev.at = cur.at.Add(-time.Second)
	next := `@@stat
cpu  1300 0 600 8500 600 0 0 0 0 0
@@net
  eth0: 1500000 100 0 0 0 0 0 0 2100000 200 0 0 0 0 0 0
@@procs
842 (my (odd) proc) R 1 842 842 0 -1 4194560 1209 3868 0 1 100 50 1 3 20 0 3 0 100 45875 1024 18446744073709551615
`
	s2, cur2 := parseMonitor(next, nil)
	applyRates(&s2, cur2, prev)
	// busy +400 over a total of +1000
	if s2.CPU < 39.9 || s2.CPU > 40.1 {
		t.Errorf("cpu %v", s2.CPU)
	}
	if s2.NetRx < 499000 || s2.NetRx > 501000 || s2.NetTx < 99000 || s2.NetTx > 101000 {
		t.Errorf("net %v %v", s2.NetRx, s2.NetTx)
	}
	// 50 ticks (0.5 s of CPU) in one second
	if p := s2.Procs[0]; p.CPU < 49 || p.CPU > 51 {
		t.Errorf("process cpu %v", p.CPU)
	}
}
