package core

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
)

// The services of a server are its systemd units, listed and driven with
// systemctl over the connection (nothing is installed). Starting and
// stopping them needs the rights of the account (root, usually).

// ServiceInfo is a systemd service of a server.
type ServiceInfo struct {
	Name        string `json:"name"` // nginx.service
	Description string `json:"description,omitempty"`
	Load        string `json:"load"`   // loaded, not-found, masked, not-loaded
	Active      string `json:"active"` // active, inactive, failed, activating, deactivating, reloading
	Sub         string `json:"sub"`    // running, exited, dead, failed, auto-restart…
	// Enabled is the state of its unit file: enabled, disabled, static,
	// masked, indirect, generated, alias…
	Enabled string `json:"enabled,omitempty"`
}

// ServiceDetails are the properties of a service and its last log lines.
type ServiceDetails struct {
	Props map[string]string `json:"props"`
	Logs  string            `json:"logs"`
	// LogsDenied: the account may not read the journal of the system.
	LogsDenied bool `json:"logsDenied,omitempty"`
}

var (
	ErrNoSystemd = errors.New("this server does not use systemd")
	ErrNeedRoot  = errors.New("only root (or a user allowed by polkit) can do this: connect with such an account")
	serviceName  = regexp.MustCompile(`^[A-Za-z0-9@._:\\-]+$`)
)

const noSystemd = "SKIFF:NOSYSTEMD"

func systemdError(out string, err error) error {
	low := strings.ToLower(out)
	if err != nil {
		low += " " + strings.ToLower(err.Error())
	}
	switch {
	case strings.Contains(out, noSystemd), strings.Contains(low, "not been booted with systemd"), strings.Contains(low, "failed to connect to bus"):
		return ErrNoSystemd
	case strings.Contains(low, "interactive authentication required"), strings.Contains(low, "access denied"), strings.Contains(low, "permission denied"):
		return ErrNeedRoot
	}
	return err
}

// Services lists the services of a server: those loaded, and the unit
// files of the others.
func (c *Core) Services(ctx context.Context, conn string) ([]ServiceInfo, error) {
	cn, ok := c.hasShell(conn)
	if !ok {
		return nil, errors.New("the services are available for SFTP and SCP connections with a shell")
	}
	cmd := `command -v systemctl >/dev/null 2>&1 || { echo ` + noSystemd + `; exit 0; }
systemctl list-units --type=service --all --no-legend --no-pager --plain 2>&1
echo SKIFF:FILES
systemctl list-unit-files --type=service --no-legend --no-pager 2>/dev/null
true`
	out, err := cn.sshOutput(ctx, cmd)
	if e := systemdError(out, err); e != nil {
		return nil, e
	}
	return parseServices(out), nil
}

func parseServices(out string) []ServiceInfo {
	units, files, _ := strings.Cut(out, "SKIFF:FILES")
	by := map[string]*ServiceInfo{}
	var list []*ServiceInfo
	for _, ln := range strings.Split(units, "\n") {
		ln = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), "●*"))
		f := strings.Fields(ln)
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		s := &ServiceInfo{Name: f[0], Load: f[1], Active: f[2], Sub: f[3], Description: strings.Join(f[4:], " ")}
		by[s.Name] = s
		list = append(list, s)
	}
	for _, ln := range strings.Split(files, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		if s, ok := by[f[0]]; ok {
			s.Enabled = f[1]
			continue
		}
		if strings.Contains(f[0], "@.") {
			continue // a template, not a service
		}
		s := &ServiceInfo{Name: f[0], Load: "not-loaded", Active: "inactive", Sub: "dead", Enabled: f[1]}
		by[s.Name] = s
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	res := make([]ServiceInfo, len(list))
	for i, s := range list {
		res[i] = *s
	}
	return res
}

// ServiceAction starts, stops, restarts or reloads a service, or turns
// its start at boot on or off.
func (c *Core) ServiceAction(ctx context.Context, conn, name, action string) error {
	switch action {
	case "start", "stop", "restart", "reload", "enable", "disable":
	default:
		return errors.New("unknown action " + action)
	}
	if !serviceName.MatchString(name) || strings.HasPrefix(name, "-") {
		return errors.New("invalid service name")
	}
	cn, ok := c.hasShell(conn)
	if !ok {
		return errors.New("the services are available for SFTP and SCP connections with a shell")
	}
	out, err := cn.sshOutput(ctx, "systemctl "+action+" --no-ask-password -- "+name+" 2>&1")
	if err != nil {
		if e := systemdError(out, err); e != err {
			return e
		}
		if msg := strings.TrimSpace(out); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// serviceProps are the properties shown for a service.
var serviceProps = []string{"Id", "Description", "LoadState", "ActiveState", "SubState", "UnitFileState", "MainPID", "MemoryCurrent", "ActiveEnterTimestamp", "InactiveEnterTimestamp", "FragmentPath", "ExecMainStatus", "Result", "NRestarts"}

// ServiceDetails gives the properties of a service and its last log lines.
func (c *Core) ServiceDetails(ctx context.Context, conn, name string, lines int) (ServiceDetails, error) {
	if !serviceName.MatchString(name) || strings.HasPrefix(name, "-") {
		return ServiceDetails{}, errors.New("invalid service name")
	}
	if lines <= 0 || lines > 2000 {
		lines = 100
	}
	cn, ok := c.hasShell(conn)
	if !ok {
		return ServiceDetails{}, errors.New("the services are available for SFTP and SCP connections with a shell")
	}
	cmd := "systemctl show --no-pager -p " + strings.Join(serviceProps, ",") + " -- " + name + " 2>&1; echo SKIFF:LOGS; " +
		"journalctl -u " + name + " -n " + itoa(lines) + " --no-pager -o short-iso 2>&1; true"
	out, err := cn.sshOutput(ctx, cmd)
	if e := systemdError(out, err); e != nil {
		return ServiceDetails{}, e
	}
	props, logs, _ := strings.Cut(out, "SKIFF:LOGS")
	d := ServiceDetails{Props: map[string]string{}}
	for _, ln := range strings.Split(props, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(ln), "="); ok {
			d.Props[k] = v
		}
	}
	logs = strings.Trim(logs, "\n")
	low := strings.ToLower(logs)
	if strings.Contains(low, "insufficient permissions") || strings.Contains(low, "not seeing messages from other users") {
		d.LogsDenied = true
	}
	d.Logs = logs
	return d, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
