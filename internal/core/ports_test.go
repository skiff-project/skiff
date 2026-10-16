package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePorts(t *testing.T) {
	ss := `SKIFF:SS
LISTEN 0      511          0.0.0.0:80        0.0.0.0:*    users:(("nginx",pid=812,fd=6),("nginx",pid=811,fd=6))
LISTEN 0      511             [::]:80           [::]:*    users:(("nginx",pid=812,fd=7))
LISTEN 0      4096   127.0.0.53%lo:53        0.0.0.0:*    users:(("systemd-resolve",pid=601,fd=14))
LISTEN 0      511        127.0.0.1:3000      0.0.0.0:*    users:(("node",pid=1400,fd=20))
LISTEN 0      128       10.0.0.5:5432        0.0.0.0:*
LISTEN 0      128                *:22              *:*
`
	got := parsePorts(ss)
	if len(got) != 5 {
		t.Fatalf("ss: %+v", got)
	}
	by := map[int]PortInfo{}
	for _, p := range got {
		by[p.Port] = p
	}
	if p := by[80]; p.Process != "nginx" || p.PID != 812 || p.Scope != "public" || strings.Join(p.Addresses, " ") != "0.0.0.0 ::" || p.Service != "HTTP" {
		t.Errorf("80 %+v", p)
	}
	if p := by[3000]; p.Scope != "local" || p.Process != "node" {
		t.Errorf("3000 %+v", p)
	}
	if p := by[53]; p.Addresses[0] != "127.0.0.53" || p.Scope != "local" {
		t.Errorf("53 %+v", p)
	}
	if p := by[5432]; p.Scope != "interface" || p.Process != "" || p.Service != "PostgreSQL" {
		t.Errorf("5432 %+v", p)
	}
	if p := by[22]; p.Scope != "public" {
		t.Errorf("22 %+v", p)
	}
	// busybox netstat
	got = parsePorts(`SKIFF:NETSTAT
Active Internet connections (only servers)
Proto Recv-Q Send-Q Local Address           Foreign Address         State       PID/Program name
tcp        0      0 0.0.0.0:2222            0.0.0.0:*               LISTEN      61/sshd.pam: /usr/s
tcp        0      0 :::2222                 :::*                    LISTEN      61/sshd.pam: /usr/s
tcp        0      0 127.0.0.1:6379          0.0.0.0:*               LISTEN      -
`)
	if len(got) != 2 || got[0].Port != 2222 || got[0].Process != "sshd.pam" || got[0].Scope != "public" || got[1].Port != 6379 || got[1].Process != "" {
		t.Errorf("netstat %+v", got)
	}
	// /proc/net/tcp and tcp6
	got = parsePorts(`SKIFF:PROC
  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2
   2: 0100007F:9C40 0100007F:0BB8 01 00000000:00000000 00:00000000 00000000  1000        0 3
   0: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 4
`)
	if len(got) != 3 || got[0].Port != 80 || got[0].Addresses[0] != "0.0.0.0" || got[1].Port != 3000 || got[1].Addresses[0] != "127.0.0.1" || got[2].Port != 8080 || got[2].Addresses[0] != "::1" {
		t.Errorf("proc %+v", got)
	}
}

func TestOpenURLRefusesOtherSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "http://", "ftp://x"} {
		if err := OpenURL(u); err == nil {
			t.Errorf("%s opened", u)
		}
	}
}

func TestParseDu(t *testing.T) {
	u := parseDu("4\t./notes.txt\ndu: cannot read directory './secret': Permission denied\n20480\t./backups\n8\t./secret\n120\t./www\n20612\t.\nSKIFF:DIRS\n./backups\n./www\n./secret\nSKIFF:DF\n/dev/vda1 25668836 13834220 10554232 57% /\n")
	if u.Total != 20612*1024 || u.Denied != 1 || len(u.Entries) != 4 || u.Entries[0].Name != "backups" || !u.Entries[0].IsDir || u.Entries[3].Name != "notes.txt" || u.Entries[3].IsDir {
		t.Errorf("du %+v", u)
	}
	if u.DiskSize != 25668836*1024 || u.DiskFree != 10554232*1024 || u.Mount != "/" {
		t.Errorf("df %+v", u)
	}
}

func TestScanUsage(t *testing.T) {
	c, root := projectCore(t)
	os.WriteFile(filepath.Join(root, "big.bin"), make([]byte, 50000), 0o644)
	u, err := c.DiskUsage(context.Background(), LocalID, root)
	if err != nil {
		t.Fatal(err)
	}
	if u.Method != "scan" || u.Entries[0].Name != "big.bin" || u.Entries[0].Size != 50000 {
		t.Fatalf("scan %+v", u.Entries)
	}
	var conf UsageEntry
	for _, e := range u.Entries {
		if e.Name == "conf" {
			conf = e
		}
	}
	if !conf.IsDir || conf.Size != int64(len("server {\n  listen 80;\n  server_name example.com;\n}\n")+len("server_name blog.example.com;\n")) {
		t.Errorf("conf %+v", conf)
	}
	var sum int64
	for _, e := range u.Entries {
		sum += e.Size
	}
	if sum != u.Total {
		t.Errorf("total %d, sum %d", u.Total, sum)
	}
}
