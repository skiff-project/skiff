package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"skiff/internal/config"
	"skiff/internal/prompt"
	"skiff/internal/sshx"
	"skiff/internal/vfs"
	"skiff/internal/vfs/scpfs"
	"skiff/internal/vfs/sftpfs"
)

// Containers are browsed with the "exec" command of their runtime — docker,
// podman or kubectl — run on this computer or on an SSH server. Nothing is
// needed in the container but a POSIX shell and the usual tools (busybox
// is enough).

// containerHost is where the runtime command runs.
type containerHost struct {
	ssh   *sshx.Conn // nil: this computer
	owned bool       // the connection was opened for the container
	label string
}

func runtimeCommand(site config.Site) string {
	if site.ContainerCommand != "" {
		return site.ContainerCommand
	}
	if site.ContainerRuntime == "" {
		return "docker"
	}
	return site.ContainerRuntime
}

// execArgv is the command that runs argv inside the container.
func execArgv(site config.Site, tty bool) []string {
	flag := "-i"
	if tty {
		flag = "-it"
	}
	cmd := runtimeCommand(site)
	if site.ContainerRuntime == "kubectl" {
		a := []string{cmd}
		if site.K8sContext != "" {
			a = append(a, "--context", site.K8sContext)
		}
		if site.K8sNamespace != "" {
			a = append(a, "-n", site.K8sNamespace)
		}
		a = append(a, "exec", flag, site.Host)
		if site.K8sContainer != "" {
			a = append(a, "-c", site.K8sContainer)
		}
		return append(a, "--")
	}
	a := []string{cmd, "exec", flag}
	if site.User != "" {
		a = append(a, "-u", site.User)
	}
	return append(a, site.Host)
}

func shellLine(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = vfs.ShellQuote(a)
	}
	return strings.Join(q, " ")
}

// containerTarget opens (or reuses) the place where the runtime runs.
func (c *Core) containerTarget(ctx context.Context, site config.Site, pr prompt.Prompter, log *Log) (*containerHost, error) {
	switch {
	case site.ViaConn != "":
		cn, err := c.Conn(site.ViaConn)
		if err != nil {
			return nil, err
		}
		s := cn.SSH()
		if s == nil {
			return nil, errors.New("containers can be reached through SFTP and SCP connections only")
		}
		return &containerHost{ssh: s, label: cn.Title}, nil
	case site.ViaSite != "":
		st, ok := c.Store.Site(site.ViaSite)
		if !ok {
			return nil, errors.New("the SSH site of the container is gone")
		}
		r, err := c.Store.Resolve(st)
		if err != nil {
			return nil, err
		}
		if r.Protocol != config.ProtoSFTP && r.Protocol != config.ProtoSCP {
			return nil, errors.New("containers can be reached through SFTP and SCP sites only")
		}
		setup, err := c.resolveSSH(r, 0)
		if err != nil {
			return nil, err
		}
		conn, err := sshx.Dial(ctx, c.sshOptions(setup, r, pr, log))
		if err != nil {
			return nil, err
		}
		return &containerHost{ssh: conn, owned: true, label: r.Title()}, nil
	}
	if _, err := exec.LookPath(runtimeCommand(site)); err != nil {
		return nil, fmt.Errorf("%s is not installed on this computer", runtimeCommand(site))
	}
	return &containerHost{label: "this computer"}, nil
}

// run executes argv on the host: locally, or as one SSH command line.
func (h *containerHost) run(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if h.ssh != nil {
		return sftpfs.ExecSSH(ctx, h.ssh.Client, shellLine(argv), stdin, stdout, stderr)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (h *containerHost) close() {
	if h.owned && h.ssh != nil {
		h.ssh.Close()
	}
}

// dialContainer opens the file system of a container.
func (c *Core) dialContainer(ctx context.Context, site config.Site, pr prompt.Prompter, log *Log) (*dialed, error) {
	if site.Host == "" {
		return nil, errors.New("no container name")
	}
	h, err := c.containerTarget(ctx, site, pr, log)
	if err != nil {
		return nil, err
	}
	prefix := execArgv(site, false)
	run := func(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		return h.run(ctx, append(append([]string(nil), prefix...), "sh", "-c", cmd), stdin, stdout, stderr)
	}
	log.Logf("Opening container %s with %s on %s", site.Host, runtimeCommand(site), h.label)
	var errb bytes.Buffer
	if code, err := run(ctx, "true", nil, io.Discard, &errb); err != nil || code != 0 {
		h.close()
		msg := strings.TrimSpace(errb.String())
		if err != nil {
			msg = err.Error()
		}
		return nil, fmt.Errorf("container %s: %s", site.Host, msg)
	}
	runtime := site.ContainerRuntime
	if runtime == "" {
		runtime = "docker"
	}
	info := []vfs.InfoItem{
		{Group: "Container", Key: "Runtime", Value: runtime},
		{Group: "Container", Key: "Name", Value: site.Host},
		{Group: "Container", Key: "Host", Value: h.label},
	}
	if site.K8sNamespace != "" {
		info = append(info, vfs.InfoItem{Group: "Container", Key: "Namespace", Value: site.K8sNamespace})
	}
	fs := scpfs.NewExec(run, scpfs.ExecOptions{
		Protocol: config.ProtoContainer, Info: info, Logf: log.Logf,
		Close: func() error { h.close(); return nil },
	})
	return &dialed{fs: fs, container: h}, nil
}

// ContainerInfo is a container (or pod) found by ListContainers.
type ContainerInfo struct {
	Name    string `json:"name"`
	Image   string `json:"image,omitempty"`
	Status  string `json:"status,omitempty"` // as the runtime words it
	Running bool   `json:"running"`
	// State is running, exited, paused, created, restarting, dead; for a
	// pod its phase in lower case (pending, running, succeeded, failed).
	State   string `json:"state,omitempty"`
	Project string `json:"project,omitempty"` // Docker Compose project
	Ports   string `json:"ports,omitempty"`
	// Kubernetes
	Namespace  string   `json:"namespace,omitempty"`
	Containers []string `json:"containers,omitempty"`
}

// ContainerRuntimes are the runtimes installed on this computer, or on
// the server behind an open SSH connection (via), in the usual order.
func (c *Core) ContainerRuntimes(ctx context.Context, via string) ([]string, error) {
	all := []string{"docker", "podman", "kubectl"}
	out := []string{}
	if via == "" {
		for _, r := range all {
			if _, err := exec.LookPath(r); err == nil {
				out = append(out, r)
			}
		}
		return out, nil
	}
	h, err := c.containerTarget(ctx, config.Site{Protocol: config.ProtoContainer, ViaConn: via}, nil, nil)
	if err != nil {
		return nil, err
	}
	defer h.close()
	var b bytes.Buffer
	if _, err := sftpfs.ExecSSH(ctx, h.ssh.Client, "for c in "+strings.Join(all, " ")+"; do command -v $c >/dev/null 2>&1 && echo $c; done; true", nil, &b, io.Discard); err != nil {
		return nil, err
	}
	found := strings.Fields(b.String())
	for _, r := range all {
		for _, f := range found {
			if f == r {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// ContainerAction starts, stops or restarts a Docker or Podman container.
func (c *Core) ContainerAction(ctx context.Context, runtime, command, via, name, action string) error {
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("unknown container action %q", action)
	}
	if runtime == "kubectl" {
		return errors.New("Kubernetes starts and stops the pods itself")
	}
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("no container name")
	}
	site := config.Site{Protocol: config.ProtoContainer, ContainerRuntime: runtime, ContainerCommand: command, ViaConn: via}
	h, err := c.containerTarget(ctx, site, nil, nil)
	if err != nil {
		return err
	}
	defer h.close()
	argv := []string{runtimeCommand(site), action, name}
	var errb bytes.Buffer
	code, err := h.run(ctx, argv, nil, io.Discard, &errb)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%s %s: %s", argv[0], action, strings.TrimSpace(errb.String()))
	}
	return nil
}

// ListContainers lists the containers of a runtime, on this computer or
// behind an open SSH connection (via).
func (c *Core) ListContainers(ctx context.Context, runtime, command, via, k8sContext, namespace string) ([]ContainerInfo, error) {
	site := config.Site{Protocol: config.ProtoContainer, ContainerRuntime: runtime, ContainerCommand: command, ViaConn: via, K8sContext: k8sContext, K8sNamespace: namespace}
	h, err := c.containerTarget(ctx, site, nil, nil)
	if err != nil {
		return nil, err
	}
	defer h.close()
	cmd := runtimeCommand(site)
	var argv []string
	if runtime == "kubectl" {
		argv = []string{cmd}
		if k8sContext != "" {
			argv = append(argv, "--context", k8sContext)
		}
		if namespace != "" {
			argv = append(argv, "-n", namespace)
		} else {
			argv = append(argv, "--all-namespaces")
		}
		argv = append(argv, "get", "pods", "-o", "json")
	} else {
		argv = []string{cmd, "ps", "-a", "--format", "{{json .}}"}
	}
	var out, errb bytes.Buffer
	code, err := h.run(ctx, argv, nil, &out, &errb)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%s: %s", strings.Join(argv[:2], " "), strings.TrimSpace(errb.String()))
	}
	if runtime == "kubectl" {
		return parsePods(out.Bytes())
	}
	return parseDockerPS(out.String()), nil
}

// psLine is a line of "ps --format {{json .}}": Docker writes the names,
// labels and ports as strings, Podman as a list, a map and a list.
type psLine struct {
	Names  json.RawMessage
	Image  string
	Status string
	State  json.RawMessage
	Labels json.RawMessage
	Ports  json.RawMessage
}

// strings of a JSON string or list of strings
func rawStrings(m json.RawMessage) []string {
	var one string
	if json.Unmarshal(m, &one) == nil {
		if one == "" {
			return nil
		}
		return []string{one}
	}
	var many []string
	if json.Unmarshal(m, &many) != nil {
		return nil // a list of objects (Podman ports)
	}
	return many
}

// label finds a label in "a=1,b=2" (Docker) or {"a":"1"} (Podman).
func label(m json.RawMessage, key string) string {
	var labels map[string]string
	if json.Unmarshal(m, &labels) == nil {
		return labels[key]
	}
	var s string
	json.Unmarshal(m, &s)
	for _, kv := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// ports of Docker ("0.0.0.0:80->80/tcp, …") or Podman (a list of mappings)
func ports(m json.RawMessage) string {
	if s := rawStrings(m); len(s) > 0 {
		return strings.Join(s, ", ")
	}
	var list []struct {
		HostIP        string `json:"host_ip"`
		HostPort      int    `json:"host_port"`
		ContainerPort int    `json:"container_port"`
		Protocol      string `json:"protocol"`
	}
	json.Unmarshal(m, &list)
	var out []string
	for _, p := range list {
		ip := p.HostIP
		if ip == "" {
			ip = "0.0.0.0"
		}
		out = append(out, fmt.Sprintf("%s:%d->%d/%s", ip, p.HostPort, p.ContainerPort, p.Protocol))
	}
	return strings.Join(out, ", ")
}

func parseDockerPS(out string) []ContainerInfo {
	res := []ContainerInfo{}
	var lines []psLine
	if t := strings.TrimSpace(out); strings.HasPrefix(t, "[") {
		json.Unmarshal([]byte(t), &lines) // podman ps --format json
	} else {
		for _, ln := range strings.Split(out, "\n") {
			var r psLine
			if json.Unmarshal([]byte(strings.TrimSpace(ln)), &r) == nil {
				lines = append(lines, r)
			}
		}
	}
	for _, r := range lines {
		names := rawStrings(r.Names)
		if len(names) == 0 {
			continue
		}
		state := strings.ToLower(strings.Join(rawStrings(r.State), ""))
		res = append(res, ContainerInfo{
			Name: strings.Split(names[0], ",")[0], Image: r.Image, Status: r.Status, State: state,
			Running: state == "running" || strings.HasPrefix(r.Status, "Up"),
			Project: label(r.Labels, "com.docker.compose.project"), Ports: ports(r.Ports),
		})
	}
	return res
}

func parsePods(data []byte) ([]ContainerInfo, error) {
	var list struct {
		Items []struct {
			Metadata struct{ Name, Namespace string }
			Spec     struct {
				Containers []struct{ Name, Image string }
			}
			Status struct{ Phase string }
		}
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("kubectl get pods: %w", err)
	}
	res := []ContainerInfo{}
	for _, it := range list.Items {
		p := ContainerInfo{Name: it.Metadata.Name, Namespace: it.Metadata.Namespace, Status: it.Status.Phase, State: strings.ToLower(it.Status.Phase), Running: it.Status.Phase == "Running"}
		for _, ct := range it.Spec.Containers {
			p.Containers = append(p.Containers, ct.Name)
			if p.Image == "" {
				p.Image = ct.Image
			}
		}
		res = append(res, p)
	}
	return res, nil
}
