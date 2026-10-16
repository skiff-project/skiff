//go:build integration

package server

import (
	"testing"
	"time"

	"skiff/internal/core"
)

// testMonitor samples the SSH test server and stops one of its processes.
func testMonitor(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	e.must(t, "fs.exec", map[string]string{"conn": ci.ID, "command": "nohup sleep 4321 >/dev/null 2>&1 &"}, nil)
	sample := func() core.MonitorSample {
		var s core.MonitorSample
		e.must(t, "monitor.sample", map[string]string{"conn": ci.ID, "key": "e2e"}, &s)
		return s
	}
	first := sample()
	if first.Hostname == "" || first.CPUs < 1 || first.MemTotal <= 0 || first.MemUsed <= 0 || first.Uptime <= 0 || first.CPU != -1 {
		t.Errorf("first sample %+v", first)
	}
	root := false
	for _, d := range first.Disks {
		root = root || (d.Mount == "/" && d.Total > 0 && d.Used > 0)
	}
	if !root {
		t.Errorf("disks %+v", first.Disks)
	}
	time.Sleep(600 * time.Millisecond)
	s := sample()
	if s.CPU < 0 || s.CPU > 100 || s.NetRx < 0 || s.NetTx < 0 || s.ProcCount < 2 {
		t.Errorf("second sample: cpu %v net %v/%v procs %d", s.CPU, s.NetRx, s.NetTx, s.ProcCount)
	}
	pid := 0
	for _, p := range s.Procs {
		if p.Name == "sleep" && p.User == "skiff" {
			pid = p.PID
		}
	}
	if pid == 0 {
		// the list keeps the 40 busiest processes: on a quiet server sleep is in it
		t.Fatalf("sleep not listed among %d processes: %+v", s.ProcCount, s.Procs)
	}
	e.must(t, "monitor.kill", map[string]any{"conn": ci.ID, "pid": pid, "signal": "TERM"}, nil)
	eventually(t, "process stopped", 5*time.Second, func() bool {
		for _, p := range sample().Procs {
			if p.PID == pid {
				return false
			}
		}
		return true
	})
	if err := e.mustFail(t, "monitor.kill", map[string]any{"conn": ci.ID, "pid": pid, "signal": "TERM"}); err == nil {
		t.Error("killing a process that is gone succeeded")
	}
	if err := e.mustFail(t, "monitor.kill", map[string]any{"conn": ci.ID, "pid": 1, "signal": "TERM; reboot"}); err == nil {
		t.Error("an arbitrary signal was accepted")
	}
	// other protocols have no shell
	ftp := e.connect(t, "ftp")
	if err := e.mustFail(t, "monitor.sample", map[string]string{"conn": ftp.ID, "key": "e2e"}); err == nil {
		t.Error("monitor on an FTP connection")
	}
}
