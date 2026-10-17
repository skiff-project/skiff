//go:build integration

package main

// End-to-end tests of the command line: every script command, the
// winscp.com compatibility, the console, keygen, import and serve, run with
// the real binary against the Docker test servers:
//
//   docker compose -f testenv/docker-compose.yml up -d
//   go test -tags integration . -run TestCLI -v

import (
	"skiff/internal/config"
	"skiff/internal/instance"
	"skiff/internal/tasks"

	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

var skiffBin string

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "skiff-cli")
	skiffBin = filepath.Join(dir, "skiff")
	if out, err := exec.Command("go", "build", "-o", skiffBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type cli struct {
	t    *testing.T
	home string // HOME and SKIFF_HOME
	env  []string
}

func newCLI(t *testing.T) *cli {
	home := t.TempDir()
	return &cli{t: t, home: home, env: append(os.Environ(), "HOME="+home, "SKIFF_HOME="+filepath.Join(home, "config"), "SSH_AUTH_SOCK=")}
}

func (c *cli) run(stdin string, args ...string) (string, int) {
	c.t.Helper()
	cmd := exec.Command(skiffBin, args...)
	cmd.Env, cmd.Dir = c.env, c.home
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		c.t.Fatal(err)
	}
	return string(out), code
}

// remote runs a shell command in the SSH test container.
func remote(t *testing.T, sh string) string {
	t.Helper()
	out, _ := exec.Command("docker", "exec", "testenv-ssh-1", "sh", "-c", sh).CombinedOutput()
	return string(out)
}

const sftpURL = "sftp://skiff:skiff@127.0.0.1:2222/"

func TestCLIVersionHelp(t *testing.T) {
	c := newCLI(t)
	if out, code := c.run("", "version"); code != 0 || !strings.HasPrefix(out, "skiff ") {
		t.Errorf("version: %d %q", code, out)
	}
	if out, code := c.run("", "help"); code != 0 || !strings.Contains(out, "skiff serve") || !strings.Contains(out, "/script=") {
		t.Errorf("help: %d %q", code, out)
	}
}

func TestCLIScriptCommands(t *testing.T) {
	c := newCLI(t)
	dir := fmt.Sprintf("/config/cli-%d", time.Now().UnixNano()%1e9)
	defer remote(t, "rm -rf "+dir)
	local := filepath.Join(c.home, "local")
	for name, content := range map[string]string{
		"a.txt": "alpha\n", "b.txt": "ligne1\nligne2\n", "c.txt": "c\n",
		"site/index.html": "<h1>x</h1>", "site/style.css": "body{}",
		"sync/un.txt": "1", "sync/deux.txt": "22",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(local, name)), 0o755)
		os.WriteFile(filepath.Join(local, name), []byte(content), 0o644)
	}
	old := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)
	os.Chtimes(filepath.Join(local, "c.txt"), old, old)
	script := fmt.Sprintf(`# toutes les commandes de script
; commentaire
option batch abort
option confirm off
option echo on
option
open %s -hostkey=*
pwd
mkdir %s
cd %s
pwd
lcd "%s"
lpwd
lls
put a.txt
put -transfer=ascii b.txt
put -filemask=*.html site
put -permissions=640 -preservetime c.txt
ls
ls *.txt
stat c.txt
chmod 600 a.txt
chmod -recursive 750 site
ln a.txt lien
ln -hard a.txt physique
mv a.txt renomme.txt
cp renomme.txt copie.txt
checksum sha256 renomme.txt
call echo appel-%%USER%%-ok
get renomme.txt telecharge/
get -delete copie.txt telecharge/
get -latest *.txt dernier/
get -neweronly renomme.txt telecharge/
put -resume c.txt
synchronize remote -criteria=size sync %s/sync
synchronize local -preview sync-retour %s/sync
synchronize both -mirror sync %s/sync
rm lien
rm site/index.html
rmdir site
echo fin-%%TIMESTAMP#yyyy%%
session
close
exit
`, sftpURL, dir, dir, local, dir, dir, dir)
	os.WriteFile(filepath.Join(c.home, "script.txt"), []byte(script), 0o644)
	out, code := c.run("", "run", "-log", filepath.Join(c.home, "session.log"), filepath.Join(c.home, "script.txt"))
	if code != 0 {
		t.Fatalf("script failed (%d):\n%s", code, out)
	}
	checks := []string{"> pwd", dir, "appel-", "-ok", "fin-" + time.Now().Format("2006"), "batch", "confirm",
		"-rw-r----- skiff    users               2 Feb  3",                              // put -permissions=640 -preservetime
		"b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060  renomme.txt", // sha256("alpha\n")
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// remote state
	ls := remote(t, "cd "+dir+" && ls -la && stat -c '%n %a %Y' c.txt renomme.txt physique && readlink lien; cat b.txt | od -c | head -2; ls sync")
	for _, want := range []string{"renomme.txt", "physique", "c.txt 644 1580702706", "-rw-------  2 skiff users    6", "un.txt", "deux.txt"} {
		if !strings.Contains(ls, want) {
			t.Errorf("remote state lacks %q:\n%s", want, ls)
		}
	}
	if strings.Contains(ls, "copie.txt") || strings.Contains(ls, "site") || strings.Contains(ls, " lien ") {
		t.Errorf("get -delete / rm / rmdir did not remove:\n%s", ls)
	}
	// local state
	for _, f := range []string{"telecharge/renomme.txt", "telecharge/copie.txt", "dernier"} {
		if _, err := os.Stat(filepath.Join(local, f)); err != nil {
			t.Errorf("local %s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(local, "sync-retour")); err == nil {
		t.Error("synchronize -preview must not transfer")
	}
	if log, _ := os.ReadFile(filepath.Join(c.home, "session.log")); !bytes.Contains(log, []byte("Authenticated")) {
		t.Errorf("session log: %s", log)
	}
}

func TestCLIBatchModes(t *testing.T) {
	c := newCLI(t)
	// abort (default): the first error stops the script, exit code 1
	out, code := c.run("", "run", "-c", "open "+sftpURL+" -hostkey=*", "-c", "cd /inexistant", "-c", "echo apres-erreur", "-c", "exit")
	if code != 1 || strings.Contains(out, "apres-erreur") {
		t.Errorf("abort: %d\n%s", code, out)
	}
	// continue: the script goes on
	out, code = c.run("", "run", "-batch", "continue", "-c", "open "+sftpURL+" -hostkey=*", "-c", "cd /inexistant", "-c", "echo apres-erreur", "-c", "exit")
	if !strings.Contains(out, "apres-erreur") {
		t.Errorf("continue: %d\n%s", code, out)
	}
	// failonnomatch
	out, code = c.run("", "run", "-c", "open "+sftpURL+" -hostkey=*", "-c", "option failonnomatch on", "-c", "get /config/*.introuvable", "-c", "exit")
	if code != 1 {
		t.Errorf("failonnomatch on: %d\n%s", code, out)
	}
	out, code = c.run("", "run", "-c", "open "+sftpURL+" -hostkey=*", "-c", "get /config/*.introuvable", "-c", "exit")
	if code != 0 {
		t.Errorf("failonnomatch off: %d\n%s", code, out)
	}
	// an unknown host key is refused in batch mode
	out, code = c.run("", "run", "-c", "open "+sftpURL, "-c", "exit")
	if code != 1 || !strings.Contains(out, "hostkey") {
		t.Errorf("unknown host key: %d\n%s", code, out)
	}
	// a wrong pinned key is refused
	out, code = c.run("", "run", "-c", "open "+sftpURL+" -hostkey=SHA256:faux", "-c", "exit")
	if code != 1 {
		t.Errorf("wrong pinned key: %d\n%s", code, out)
	}
	// other protocols
	for _, u := range []string{
		"ftpes://skiff:skiff@127.0.0.1:2121/ -certificate=*",
		"ftp://skiff:skiff@127.0.0.1:2121/",
		"dav://skiff:skiff@127.0.0.1:8080/",
		"s3plain://skiffadmin:skiffsecret@127.0.0.1:9000/",
		"scp://skiff:skiff@127.0.0.1:2222/ -hostkey=*",
	} {
		out, code = c.run("", "run", "-c", "open "+u, "-c", "ls", "-c", "exit")
		if code != 0 {
			t.Errorf("open %s: %d\n%s", u, code, out)
		}
	}
}

func TestCLIWinSCPCompat(t *testing.T) {
	c := newCLI(t)
	os.WriteFile(filepath.Join(c.home, "param.txt"), []byte("open "+sftpURL+" -hostkey=*\necho parametre=%1%\nls %2%\nexit\n"), 0o644)
	out, code := c.run("", "/script="+filepath.Join(c.home, "param.txt"), "/log="+filepath.Join(c.home, "w.log"), "/parameter", "//", "valeur", "/config")
	if code != 0 || !strings.Contains(out, "parametre=valeur") || !strings.Contains(out, "www") && !strings.Contains(out, "logs") {
		t.Errorf("/script /parameter: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(c.home, "w.log")); err != nil {
		t.Error("/log not written")
	}
	out, code = c.run("", "/command", "open "+sftpURL+" -hostkey=*", "pwd", "exit")
	if code != 0 || !strings.Contains(out, "/config") {
		t.Errorf("/command: %d\n%s", code, out)
	}
	ini := filepath.Join(c.home, "autre-config")
	os.MkdirAll(ini, 0o700)
	out, code = c.run("", "/ini="+ini, "/command", "open "+sftpURL+" -hostkey=*", "exit")
	if code != 0 {
		t.Errorf("/ini: %d\n%s", code, out)
	}
}

func TestCLIKeepUpToDate(t *testing.T) {
	c := newCLI(t)
	dir := fmt.Sprintf("/config/kutd-%d", time.Now().UnixNano()%1e9)
	defer remote(t, "rm -rf "+dir)
	local := filepath.Join(c.home, "w")
	os.MkdirAll(local, 0o755)
	remote(t, "mkdir -p "+dir)
	cmd := exec.Command(skiffBin, "run", "-c", "open "+sftpURL+" -hostkey=*", "-c", fmt.Sprintf("keepuptodate -delete %s %s", local, dir))
	cmd.Env = c.env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	wait := func(what string, cond func() bool) {
		t.Helper()
		for i := 0; i < 100; i++ {
			if cond() {
				return
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatalf("timed out: %s\n%s", what, buf.String())
	}
	wait("watching", func() bool { return strings.Contains(buf.String(), "Watching") })
	os.WriteFile(filepath.Join(local, "vivant.txt"), []byte("v"), 0o644)
	wait("upload", func() bool { return strings.Contains(remote(t, "cat "+dir+"/vivant.txt"), "v") })
	os.Remove(filepath.Join(local, "vivant.txt"))
	wait("deletion", func() bool { return strings.Contains(remote(t, "ls "+dir+"/vivant.txt"), "No such file") })
	cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("keepuptodate did not stop on Ctrl+C")
	}
}

func TestCLIShell(t *testing.T) {
	c := newCLI(t)
	out, code := c.run("open "+sftpURL+" -hostkey=*\npwd\nhelp\nexit\n", "shell")
	if code != 0 || !strings.Contains(out, "interactive console") || !strings.Contains(out, "/config") || !strings.Contains(out, "synchronize") {
		t.Errorf("shell: %d\n%s", code, out)
	}
	// without a terminal, an unknown host key is refused like in a script
	out, _ = c.run("pwd\nexit\n", "shell", "sftp://skiff:skiff@127.0.0.1:2222/")
	if !strings.Contains(out, "-hostkey") {
		t.Errorf("shell without a terminal: %s", out)
	}
	// in a terminal, the console asks
	cmd := exec.Command(skiffBin, "shell", "sftp://skiff:skiff@127.0.0.1:2222/")
	cmd.Env, cmd.Dir = c.env, c.home
	f, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.Close(); cmd.Process.Kill(); cmd.Wait() }()
	var buf bytes.Buffer
	go io.Copy(&buf, f)
	waitFor := func(s string) {
		t.Helper()
		for i := 0; i < 100 && !strings.Contains(buf.String(), s); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !strings.Contains(buf.String(), s) {
			t.Fatalf("console: no %q in %q", s, buf.String())
		}
	}
	waitFor("1) Accept")
	io.WriteString(f, "2\n") // connect once
	waitFor("skiff@127.0.0.1:/config>")
	io.WriteString(f, "exit\n")
	cmd.Wait()
}

func TestCLIKeygenAndImport(t *testing.T) {
	c := newCLI(t)
	key := filepath.Join(c.home, "cle")
	out, code := c.run("", "keygen", "-N", "-t", "ed25519", "-C", "test@cli", "-f", key)
	if code != 0 || !strings.Contains(out, "SHA256:") {
		t.Fatalf("keygen: %d %s", code, out)
	}
	if b, _ := os.ReadFile(key + ".pub"); !strings.HasPrefix(string(b), "ssh-ed25519 ") || !strings.Contains(string(b), "test@cli") {
		t.Errorf("public key = %s", b)
	}
	if _, code := c.run("", "keygen", "-N", "-f", key); code == 0 {
		t.Error("keygen must refuse to overwrite")
	}
	for _, args := range [][]string{{"-t", "rsa", "-b", "2048", "-ppk", "-f", key + "-rsa.ppk"}, {"-t", "ecdsa", "-b", "521", "-f", key + "-ec"}} {
		if out, code := c.run("", append([]string{"keygen", "-N"}, args...)...); code != 0 {
			t.Errorf("keygen %v: %s", args, out)
		}
	}
	if out, code := c.run("", "keygen", "-N", "-convert", key, "-ppk"); code != 0 || !strings.Contains(out, key+".ppk") {
		t.Errorf("convert to ppk: %s", out)
	}
	if out, code := c.run("", "keygen", "-N", "-convert", key+"-rsa.ppk", "-f", key+"-rsa-openssh"); code != 0 {
		t.Errorf("convert to openssh: %s", out)
	}
	if b, _ := os.ReadFile(key + "-rsa-openssh"); !bytes.Contains(b, []byte("OPENSSH PRIVATE KEY")) {
		t.Error("converted key is not OpenSSH")
	}
	// import
	out, code = c.run("", "import")
	if code != 2 || !strings.Contains(out, "ssh-config") {
		t.Errorf("import usage: %d %s", code, out)
	}
	cfg := filepath.Join(c.home, "ssh_config")
	os.WriteFile(cfg, []byte("Host web\n  HostName 127.0.0.1\n  Port 2222\n  User skiff\n"), 0o644)
	out, code = c.run("", "import", "ssh-config", cfg)
	if code != 0 || !strings.Contains(out, "1 site(s) imported") {
		t.Errorf("import: %d %s", code, out)
	}
	urls := filepath.Join(c.home, "urls.txt")
	os.WriteFile(urls, []byte(sftpURL+"\n"), 0o644)
	if out, code = c.run("", "import", "urls", urls); code != 0 {
		t.Errorf("import urls: %s", out)
	}
	// a saved site can be opened by name in a script
	out, code = c.run("", "run", "-c", "open web -hostkey=* -password=skiff", "-c", "pwd", "-c", "exit")
	if code != 0 || !strings.Contains(out, "/config") {
		t.Errorf("open by site name: %d %s", code, out)
	}
}

func freeTCPPort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestCLISSHConfig opens a host of ~/.ssh/config by its alias, as
// "ssh alias" would, from a script and from winscp.com-style arguments.
func TestCLISSHConfig(t *testing.T) {
	c := newCLI(t)
	c.env = append(c.env, "SKIFF_SSH_SYSTEM_CONFIG=none")
	os.MkdirAll(filepath.Join(c.home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(c.home, ".ssh", "config"), []byte("Host cli-box\n    HostName 127.0.0.1\n    Port 2222\n    User skiff\n    StrictHostKeyChecking accept-new\n"), 0o600)
	out, code := c.run("", "run", "-c", "open cli-box -password=skiff", "-c", "pwd", "-c", "exit")
	if code != 0 || !strings.Contains(out, "Active session: [1] cli-box") || !strings.Contains(out, "/config") {
		t.Errorf("open alias: %d %s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(c.home, "config", "known_hosts")); !strings.Contains(string(b), "[127.0.0.1]:2222") {
		t.Errorf("accept-new did not record the key: %s", b)
	}
	out, code = c.run("", "run", "-c", "open sftp://cli-box/ -password=skiff", "-c", "exit")
	if code != 0 {
		t.Errorf("open sftp://alias: %d %s", code, out)
	}
}

// TestCLIAgent logs in from a script with a key that only a real ssh-agent
// holds. The key is certified by the test server's authority, so nothing is
// added to authorized_keys (the API tests change it at the same time).
func TestCLIAgent(t *testing.T) {
	c := newCLI(t)
	dir, err := os.MkdirTemp("", "skiff-agent") // short path for the socket
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "agent.sock")
	agent := exec.Command("ssh-agent", "-D", "-a", sock)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { agent.Process.Kill(); agent.Wait() }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	ca, key := filepath.Join(dir, "ca"), filepath.Join(dir, "id_cli")
	os.WriteFile(ca, []byte(remote(t, "cat /config/ssh_ca/user_ca")), 0o600)
	for _, args := range [][]string{
		{"ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key},
		{"ssh-keygen", "-q", "-s", ca, "-I", "cli@e2e", "-n", "skiff", key + ".pub"},
		{"env", "SSH_AUTH_SOCK=" + sock, "ssh-add", key}, // the key and its certificate
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", args[0], err, out)
		}
	}
	for _, f := range []string{key, key + ".pub", key + "-cert.pub"} {
		os.Remove(f)
	}
	script := []string{"run", "-c", "open sftp://skiff@127.0.0.1:2222/ -hostkey=*", "-c", "pwd", "-c", "exit"}
	if out, code := c.run("", script...); code == 0 {
		t.Errorf("logged in without the agent: %s", out)
	}
	c.env = append(c.env, "SSH_AUTH_SOCK="+sock)
	if out, code := c.run("", script...); code != 0 || !strings.Contains(out, "/config") {
		t.Errorf("open with the agent: %d %s", code, out)
	}
}

// TestCLITask runs a scheduled task from the command line, as the system
// scheduler would.
func TestCLITask(t *testing.T) {
	c := newCLI(t)
	store, err := config.Open(filepath.Join(c.home, "config"), false)
	if err != nil {
		t.Fatal(err)
	}
	ts := tasks.OpenStore(store)
	if _, err := ts.Save(tasks.Task{Name: "Lister la racine", Kind: "script", Enabled: true, Schedule: tasks.Schedule{Kind: "daily", At: "02:00"},
		Script: "open sftp://skiff:skiff@127.0.0.1:2222/ -hostkey=*\nls /\nexit\n"}); err != nil {
		t.Fatal(err)
	}
	if out, code := c.run("", "task", "list"); code != 0 || !strings.Contains(out, "Lister la racine") || !strings.Contains(out, "daily at 02:00") {
		t.Errorf("list: %d %s", code, out)
	}
	out, code := c.run("", "task", "run", "lister la racine")
	if code != 0 || !strings.Contains(out, "config") || !strings.Contains(out, "OK: Script finished") {
		t.Errorf("run: %d %s", code, out)
	}
	if runs := tasks.OpenStore(store).Runs(""); len(runs) != 1 || runs[0].Trigger != "command line" || runs[0].Status != "ok" {
		t.Errorf("history %+v", runs)
	}
	if _, code := c.run("", "task", "run", "absente"); code != 1 {
		t.Errorf("unknown task: exit %d", code)
	}
}

func TestCLIServe(t *testing.T) {
	c := newCLI(t)
	// no password and no terminal: refuse to start
	if out, code := c.run("", "serve", "--no-tls", "--listen", "127.0.0.1:0"); code == 0 || !strings.Contains(out, "set-password") {
		t.Errorf("serve without password: %d %s", code, out)
	}
	start := func(args ...string) (*exec.Cmd, *bytes.Buffer) {
		cmd := exec.Command(skiffBin, append([]string{"serve"}, args...)...)
		cmd.Env = append(c.env, "WEBPW=mot-de-passe-web")
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		return cmd, &buf
	}
	status := func(client *http.Client, base string) map[string]any {
		for i := 0; i < 50; i++ {
			resp, err := client.Get(base + "/api/auth/status")
			if err == nil {
				var m map[string]any
				json.NewDecoder(resp.Body).Decode(&m)
				resp.Body.Close()
				return m
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("server did not start")
		return nil
	}
	// plain HTTP behind a proxy, password from the environment
	port := freeTCPPort(t)
	_, out := start("--no-tls", "--listen", fmt.Sprintf("127.0.0.1:%d", port), "--user", "alex", "--password-env", "WEBPW", "--root", c.home)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if m := status(http.DefaultClient, base); m["mode"] != "web" || m["authenticated"] != false {
		t.Errorf("status = %v", m)
	}
	resp, err := http.Post(base+"/api/auth/login", "application/json", strings.NewReader(`{"user":"alex","password":"mot-de-passe-web"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login: %v %v", resp, err)
	}
	resp.Body.Close()
	if !strings.Contains(out.String(), "restricted to") {
		t.Errorf("--root not reported: %s", out)
	}
	// HTTPS with a generated certificate
	port = freeTCPPort(t)
	_, out = start("--listen", fmt.Sprintf("127.0.0.1:%d", port), "--password-env", "WEBPW")
	insecure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if m := status(insecure, fmt.Sprintf("https://127.0.0.1:%d", port)); m["mode"] != "web" {
		t.Errorf("https status = %v", m)
	}
	if !strings.Contains(out.String(), "Self-signed certificate SHA-256") {
		t.Errorf("certificate fingerprint not printed: %s", out)
	}
	if _, err := os.Stat(filepath.Join(c.home, "config", "web-cert.pem")); err != nil {
		t.Error("certificate not saved")
	}
	// the saved credentials are reused without --password-env
	cmd := exec.Command(skiffBin, "serve", "--set-password")
	cmd.Env = c.env
	cmd.Stdin = strings.NewReader("")
	if b, _ := cmd.CombinedOutput(); !strings.Contains(string(b), "terminal") {
		t.Errorf("--set-password without a terminal: %s", b)
	}
}

func TestCLIStartupURL(t *testing.T) {
	c := newCLI(t)
	port := freeTCPPort(t)
	cmd := exec.Command(skiffBin, "sftp://skiff@127.0.0.1:2222/config")
	cmd.Env = append(c.env, "SKIFF_NO_WINDOW=1", fmt.Sprintf("SKIFF_PORT=%d", port), "SKIFF_DEV_TOKEN=tok")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	for i := 0; i < 50; i++ {
		if resp, err := client.Get(base + "/?token=tok"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	req, _ := http.NewRequest("POST", base+"/api/rpc/app.info", strings.NewReader("{}"))
	req.Header.Set("X-Skiff", "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Result map[string]any `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	resp.Body.Close()
	if r.Result["startupUrl"] != "sftp://skiff@127.0.0.1:2222/config" || r.Result["mode"] != "desktop" {
		t.Errorf("app.info = %v", r.Result)
	}
}

// startWindow starts the window of Skiff without its webview (the local
// server only) and returns a client logged in to it.
func (c *cli) startWindow(t *testing.T, port int, token string) (*exec.Cmd, *http.Client, string) {
	t.Helper()
	cmd := exec.Command(skiffBin)
	cmd.Env = append(c.env, "SKIFF_NO_WINDOW=1", fmt.Sprintf("SKIFF_PORT=%d", port), "SKIFF_DEV_TOKEN="+token)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	for i := 0; ; i++ {
		if resp, err := client.Get(base + "/?token=" + token); err == nil {
			resp.Body.Close()
			break
		}
		if i == 100 {
			cmd.Process.Kill()
			t.Fatal("the window did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cmd, client, base
}

func rpcResult(t *testing.T, client *http.Client, base, method string, out any) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/rpc/"+method, strings.NewReader("{}"))
	req.Header.Set("X-Skiff", "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var r struct {
		Result json.RawMessage `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if err := json.Unmarshal(r.Result, out); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

// TestCLISecondStart: a second start of the window on the same
// configuration folder (a link clicked in a browser) hands its URL to the
// running one and ends; after a crash, the stale file does not stop the
// next start.
func TestCLISecondStart(t *testing.T) {
	c := newCLI(t)
	cfg := filepath.Join(c.home, "config")
	first, client, base := c.startWindow(t, freeTCPPort(t), "tok")
	defer func() { first.Process.Kill(); first.Wait() }()
	eventually := func(what string, cond func() bool) {
		t.Helper()
		for i := 0; i < 100 && !cond(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !cond() {
			t.Fatal(what)
		}
	}
	eventually("instance.json written", func() bool {
		_, err := os.Stat(filepath.Join(cfg, instance.FileName))
		return err == nil
	})
	if st, _ := os.Stat(filepath.Join(cfg, instance.FileName)); st.Mode().Perm() != 0o600 {
		t.Errorf("instance.json mode %v", st.Mode().Perm())
	}
	second := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, skiffBin, args...)
		// a window of its own would listen there: it must not get that far
		cmd.Env = append(c.env, "SKIFF_NO_WINDOW=1", fmt.Sprintf("SKIFF_PORT=%d", freeTCPPort(t)))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	start := time.Now()
	if out, err := second("--link", "sftp://skiff@127.0.0.1:2222/config"); err != nil || strings.Contains(out, "Skiff UI") {
		t.Fatalf("second start: %v %s", err, out)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("second start took %v", d)
	}
	var opens []instance.Request
	rpcResult(t, client, base, "app.takeOpens", &opens)
	if len(opens) != 1 || opens[0].URL != "sftp://skiff@127.0.0.1:2222/config" || !opens[0].Link {
		t.Errorf("opens: %+v", opens)
	}
	// without a URL: the running window comes to the front, nothing opens
	if out, err := second(); err != nil {
		t.Fatalf("plain second start: %v %s", err, out)
	}
	rpcResult(t, client, base, "app.takeOpens", &opens)
	if len(opens) != 0 {
		t.Errorf("opens after a plain start: %+v", opens)
	}

	// a crash leaves the file: the next start notices and runs
	first.Process.Signal(syscall.SIGKILL)
	first.Wait()
	if _, err := os.Stat(filepath.Join(cfg, instance.FileName)); err != nil {
		t.Fatal("the file of the crashed window is expected to stay")
	}
	port := freeTCPPort(t)
	next, client, base := c.startWindow(t, port, "tok2")
	defer func() { next.Process.Kill(); next.Wait() }()
	eventually("instance.json rewritten", func() bool {
		b, _ := os.ReadFile(filepath.Join(cfg, instance.FileName))
		return strings.Contains(string(b), fmt.Sprintf(`"port":%d`, port))
	})
	var info map[string]any
	rpcResult(t, client, base, "app.info", &info)
	if info["mode"] != "desktop" {
		t.Errorf("app.info: %v", info)
	}
	// a clean end removes the file
	next.Process.Signal(syscall.SIGTERM)
	next.Wait()
	if _, err := os.Stat(filepath.Join(cfg, instance.FileName)); !os.IsNotExist(err) {
		t.Errorf("instance.json left after the end: %v", err)
	}
}
