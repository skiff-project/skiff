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

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/transfer"
)

// testContainers browses containers: a real Docker container of this
// computer, Docker on the SSH server (a stand-in docker command) and a
// Kubernetes pod (a stand-in kubectl that checks its arguments).
func testContainers(t *testing.T, e *e2e) {
	t.Run("localDocker", func(t *testing.T) {
		const ctr = "testenv-ssh-1" // a container of the test environment
		var list []core.ContainerInfo
		e.must(t, "containers.list", map[string]string{"runtime": "docker"}, &list)
		found := false
		for _, c := range list {
			found = found || (c.Name == ctr && c.Running && c.Image != "")
		}
		if !found {
			t.Fatalf("%s not listed: %+v", ctr, list)
		}
		var ci core.ConnInfo
		e.must(t, "conn.open", map[string]any{"site": config.Site{Protocol: "container", Host: ctr, ContainerRuntime: "docker", Name: ctr}}, &ci)
		t.Cleanup(func() { e.call("conn.close", map[string]string{"conn": ci.ID}, nil) })
		if ci.Protocol != "container" || ci.Caps.Protocol != "container" || !ci.Caps.Shell {
			t.Errorf("conn %+v", ci)
		}
		dir := "/tmp/" + uniq("skiff-ctr")
		e.must(t, "fs.mkdir", map[string]any{"conn": ci.ID, "path": dir}, nil)
		defer e.call("fs.delete", map[string]any{"conn": ci.ID, "paths": []string{dir}}, nil)
		e.put(t, ci.ID, dir+"/notes.txt", "dans le conteneur\n")
		if got := e.readText(t, ci.ID, dir+"/notes.txt"); got != "dans le conteneur\n" {
			t.Errorf("read back %q", got)
		}
		e.must(t, "fs.rename", map[string]string{"conn": ci.ID, "from": dir + "/notes.txt", "to": dir + "/renamed.txt"}, nil)
		var lr core.ListResult
		e.must(t, "fs.list", map[string]string{"conn": ci.ID, "path": dir}, &lr)
		if len(lr.Entries) != 1 || lr.Entries[0].Name != "renamed.txt" || lr.Entries[0].Size != 18 {
			t.Errorf("list %+v", lr.Entries)
		}
		// a binary file there and back, through the transfer queue
		data := make([]byte, 3<<20+77)
		rand.Read(data)
		local := filepath.Join(e.home, uniq("ctr"))
		os.MkdirAll(local, 0o755)
		os.WriteFile(filepath.Join(local, "blob.bin"), data, 0o644)
		j := e.enqueue(t, transfer.Request{Src: "local", Dst: ci.ID, Paths: []string{filepath.Join(local, "blob.bin")}, DstDir: dir, Options: e.defaults(t, "local", ci.ID)})
		if j.Status != transfer.Done {
			t.Fatalf("upload %+v", j)
		}
		var sum string
		e.must(t, "fs.checksum", map[string]string{"conn": ci.ID, "path": dir + "/blob.bin", "algo": "sha256"}, &sum)
		want := sha256.Sum256(data)
		if sum != hex.EncodeToString(want[:]) {
			t.Errorf("sha256 in the container %s", sum)
		}
		back := filepath.Join(local, "back")
		if j := e.enqueue(t, transfer.Request{Src: ci.ID, Dst: "local", Paths: []string{dir + "/blob.bin"}, DstDir: back, Options: e.defaults(t, ci.ID, "local")}); j.Status != transfer.Done {
			t.Fatalf("download %+v", j)
		}
		if b, _ := os.ReadFile(filepath.Join(back, "blob.bin")); sha256.Sum256(b) != want {
			t.Error("downloaded file differs")
		}
		// a shell in the container, and the monitor of its processes
		var term termInfo
		e.must(t, "term.open", map[string]any{"conn": ci.ID, "cols": 100, "rows": 30, "cwd": dir}, &term)
		send, wait, done := e.termSession(t, term.ID)
		send("pwd; echo dans-$((40+2))\n")
		if out := wait("dans-42"); !strings.Contains(out, dir) {
			t.Errorf("terminal not in %s:\n%s", dir, out)
		}
		done()
		e.must(t, "term.close", map[string]string{"id": term.ID}, nil)
		var m core.MonitorSample
		e.must(t, "monitor.sample", map[string]string{"conn": ci.ID, "key": "ctr"}, &m)
		if m.ProcCount == 0 || m.MemTotal == 0 {
			t.Errorf("monitor in the container %+v", m)
		}
		// the same container as a URL
		var u core.ConnInfo
		e.must(t, "conn.open", map[string]any{"url": "docker://" + ctr + dir}, &u)
		if u.Home == "" {
			t.Errorf("url conn %+v", u)
		}
		e.call("conn.close", map[string]string{"conn": u.ID}, nil)
		if err := e.mustFail(t, "conn.open", map[string]any{"site": config.Site{Protocol: "container", Host: "skiff-no-such-container", ContainerRuntime: "docker"}}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "no such container") {
			t.Errorf("missing container: %v", err)
		}
	})

	t.Run("dockerOverSSH", func(t *testing.T) {
		ssh := e.connect(t, "sftp")
		home := ssh.Home
		// stands in for docker on the server: "exec" runs the command there
		fake := `#!/bin/sh
case "$1" in
ps) echo '{"Names":"web","Image":"nginx:1.27","Status":"Up 2 hours","State":"running"}'
    echo '{"Names":"old","Image":"redis:7","Status":"Exited (0) 3 days ago","State":"exited"}' ;;
exec) shift; while [ "$1" = "-i" ] || [ "$1" = "-it" ]; do shift; done
    [ "$1" = "-u" ] && shift 2
    echo "$1" >> "$HOME/fake-docker.log"; shift; exec "$@" ;;
esac
`
		e.put(t, ssh.ID, home+"/fake-docker", fake)
		mode := uint32(0o755)
		e.must(t, "fs.setAttrs", map[string]any{"conn": ssh.ID, "paths": []string{home + "/fake-docker"}, "attrs": core.Attrs{Mode: &mode}}, nil)
		var list []core.ContainerInfo
		e.must(t, "containers.list", map[string]string{"runtime": "docker", "command": home + "/fake-docker", "via": ssh.ID}, &list)
		if len(list) != 2 || list[0].Name != "web" || !list[0].Running || list[1].Running || list[0].Image != "nginx:1.27" {
			t.Fatalf("list %+v", list)
		}
		site := config.Site{Protocol: "container", Host: "web", ContainerRuntime: "docker", ContainerCommand: home + "/fake-docker", ViaConn: ssh.ID}
		var ci core.ConnInfo
		e.must(t, "conn.open", map[string]any{"site": site}, &ci)
		t.Cleanup(func() { e.call("conn.close", map[string]string{"conn": ci.ID}, nil) })
		var lr core.ListResult
		e.must(t, "fs.list", map[string]string{"conn": ci.ID, "path": home}, &lr)
		has := false
		for _, en := range lr.Entries {
			has = has || en.Name == "fake-docker"
		}
		if !has {
			t.Errorf("listing through the stand-in docker: %+v", lr.Entries)
		}
		if log := e.readText(t, ssh.ID, home+"/fake-docker.log"); !strings.Contains(log, "web\n") {
			t.Errorf("the container name was not passed: %q", log)
		}
		// through a saved SSH site: Skiff opens its own connection
		saved := e.saveSite(t, testSite("sftp"), "skiff")
		site.ViaConn, site.ViaSite = "", saved.ID
		var c2 core.ConnInfo
		e.must(t, "conn.open", map[string]any{"site": site}, &c2)
		e.call("conn.close", map[string]string{"conn": c2.ID}, nil)
		e.call("fs.delete", map[string]any{"conn": ssh.ID, "paths": []string{home + "/fake-docker", home + "/fake-docker.log"}}, nil)
	})

	t.Run("kubernetes", func(t *testing.T) {
		bin := filepath.Join(filepath.Dir(e.home), "bin")
		args := filepath.Join(filepath.Dir(e.home), "kubectl-args.txt")
		fake := `#!/bin/sh
echo "$@" >> ` + args + `
for a in "$@"; do
  if [ "$a" = "pods" ]; then cat <<'JSON'
{"items":[{"metadata":{"name":"api-7f9c","namespace":"demo"},"spec":{"containers":[{"name":"web","image":"nginx:1.27"},{"name":"sidecar","image":"envoy"}]},"status":{"phase":"Running"}}]}
JSON
  exit 0; fi
done
while [ "$1" != "--" ]; do shift; done
shift
exec "$@"
`
		os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fake), 0o755)
		var list []core.ContainerInfo
		e.must(t, "containers.list", map[string]string{"runtime": "kubectl", "context": "staging", "namespace": "demo"}, &list)
		if len(list) != 1 || list[0].Name != "api-7f9c" || list[0].Namespace != "demo" || len(list[0].Containers) != 2 || !list[0].Running {
			t.Fatalf("pods %+v", list)
		}
		var ci core.ConnInfo
		e.must(t, "conn.open", map[string]any{"url": "kubectl://api-7f9c/?namespace=demo&context=staging&container=web"}, &ci)
		e.call("conn.close", map[string]string{"conn": ci.ID}, nil)
		b, _ := os.ReadFile(args)
		if !strings.Contains(string(b), "--context staging -n demo get pods -o json") || !strings.Contains(string(b), "--context staging -n demo exec -i api-7f9c -c web -- sh -c true") {
			t.Errorf("kubectl arguments:\n%s", b)
		}
	})
}
