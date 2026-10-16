//go:build integration

package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/transfer"
)

// testSegments copies big files in parallel parts — upload, download and
// SFTP to SFTP — and checks every byte arrived.
func testSegments(t *testing.T, e *e2e) {
	ci := e.connect(t, "sftp")
	other := e.connect(t, "sftp")
	root := e.remoteRoot(t, ci)
	local := filepath.Join(e.home, uniq("segments"))
	os.MkdirAll(local, 0o755)
	data := make([]byte, 13<<20+12345) // parts of different sizes
	rand.Read(data)
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	big := filepath.Join(local, "big.bin")
	os.WriteFile(big, data, 0o644)

	var p config.Prefs
	e.must(t, "prefs.get", nil, &p)
	if p.Queue.SegmentSize != 100<<20 || p.Queue.Segments != 4 {
		t.Errorf("default segments: %d bytes, %d parts", p.Queue.SegmentSize, p.Queue.Segments)
	}
	opts := func(src, dst string) transfer.Options {
		o := e.defaults(t, src, dst)
		o.SegmentSize, o.Segments = 4<<20, 4
		return o
	}
	remoteSum := func(conn, p string) string {
		var s string
		e.must(t, "fs.checksum", map[string]string{"conn": conn, "path": p, "algo": "sha256"}, &s)
		return s
	}
	logHas := func(conn, s string) bool {
		var lines []core.LogLine
		e.must(t, "conn.log", map[string]string{"conn": conn}, &lines)
		for _, l := range lines {
			if strings.Contains(l.Msg, s) {
				return true
			}
		}
		return false
	}

	t.Run("upload", func(t *testing.T) {
		j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{big}, DstDir: root, Options: opts("local", ci.ID)})
		if j.Status != transfer.Done || j.SegmentedFiles != 1 || j.DoneBytes != int64(len(data)) {
			t.Fatalf("job %+v", j)
		}
		if got := remoteSum(ci.ID, root+"/big.bin"); got != want {
			t.Errorf("uploaded sha256 %s, want %s", got, want)
		}
		if !logHas(ci.ID, "big.bin: 4 parts copied in parallel") {
			t.Error("the connection log does not mention the parts")
		}
		if e.exists(ci.ID, root+"/big.bin.filepart") {
			t.Error(".filepart left behind")
		}
	})
	t.Run("download", func(t *testing.T) {
		back := filepath.Join(local, "back")
		j := e.enqueue(t, transfer.Request{Src: ci.ID, Dst: "local", Paths: []string{root + "/big.bin"}, DstDir: back, Options: opts(ci.ID, "local")})
		if j.Status != transfer.Done || j.SegmentedFiles != 1 {
			t.Fatalf("job %+v", j)
		}
		b, _ := os.ReadFile(filepath.Join(back, "big.bin"))
		if s := sha256.Sum256(b); hex.EncodeToString(s[:]) != want {
			t.Errorf("downloaded file differs (%d bytes)", len(b))
		}
	})
	t.Run("serverToServer", func(t *testing.T) {
		dst := e.remoteRoot(t, other)
		j := e.enqueue(t, transfer.Request{Src: ci.ID, Dst: other.ID, Paths: []string{root + "/big.bin"}, DstDir: dst, Options: opts(ci.ID, other.ID)})
		if j.Status != transfer.Done || j.SegmentedFiles != 1 {
			t.Fatalf("job %+v", j)
		}
		if got := remoteSum(other.ID, dst+"/big.bin"); got != want {
			t.Errorf("copied sha256 %s", got)
		}
	})
	t.Run("notSplit", func(t *testing.T) {
		small := filepath.Join(local, "small.bin")
		os.WriteFile(small, data[:3<<20], 0o644)
		o := opts("local", ci.ID)
		if j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{small}, DstDir: root, Options: o}); j.SegmentedFiles != 0 {
			t.Error("a file under the threshold was split")
		}
		o.SegmentSize = 0
		if j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{big}, DstDir: root + "/off", Options: o}); j.Status != transfer.Done || j.SegmentedFiles != 0 {
			t.Errorf("split although turned off: %+v", j)
		}
	})
	t.Run("cancelLeavesNoPart", func(t *testing.T) {
		o := opts("local", ci.ID)
		o.SpeedLimit = 1024 // KiB/s: the copy takes a while
		var id string
		e.must(t, "transfer.enqueue", transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{big}, DstDir: root + "/cancel", Options: o}, &id)
		eventually(t, "parts running", 15*time.Second, func() bool {
			var jobs []transfer.Snapshot
			e.must(t, "transfer.list", nil, &jobs)
			for _, j := range jobs {
				if j.ID == id && len(j.Active) == 1 && j.Active[0].Parts == 4 && j.DoneBytes > 0 {
					return true
				}
			}
			return false
		})
		e.must(t, "transfer.cancel", map[string]string{"id": id}, nil)
		if j := e.waitJob(t, id); j.Status != transfer.Cancelled {
			t.Fatalf("job %s", j.Status)
		}
		eventually(t, "partial file removed", 5*time.Second, func() bool {
			return !e.exists(ci.ID, root+"/cancel/big.bin.filepart") && !e.exists(ci.ID, root+"/cancel/big.bin")
		})
	})
}
