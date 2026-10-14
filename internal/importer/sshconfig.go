package importer

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"skiff/internal/config"
)

type sshHost struct {
	patterns []string
	opts     map[string][]string
	order    []string
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home(), p[2:])
	}
	return p
}

func readSSHConfig(data []byte, dir string, depth int) []*sshHost {
	var hosts []*sshHost
	var cur *sshHost
	skip := false
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		var key, val string
		if i := strings.IndexAny(ln, " \t="); i > 0 {
			key = strings.ToLower(ln[:i])
			val = strings.TrimSpace(strings.TrimLeft(ln[i:], " \t="))
		} else {
			continue
		}
		val = strings.Trim(val, `"`)
		switch key {
		case "host":
			cur = &sshHost{patterns: strings.Fields(val), opts: map[string][]string{}}
			hosts = append(hosts, cur)
			skip = false
		case "match":
			cur, skip = nil, true
		case "include":
			if depth > 8 {
				continue
			}
			for _, pat := range strings.Fields(val) {
				pat = expandTilde(pat)
				if !filepath.IsAbs(pat) {
					pat = filepath.Join(dir, pat)
				}
				matches, _ := filepath.Glob(pat)
				for _, m := range matches {
					if b, err := os.ReadFile(m); err == nil {
						hosts = append(hosts, readSSHConfig(b, dir, depth+1)...)
					}
				}
			}
		default:
			if cur == nil || skip {
				continue
			}
			if _, seen := cur.opts[key]; !seen {
				cur.order = append(cur.order, key)
			}
			cur.opts[key] = append(cur.opts[key], val)
		}
	}
	return hosts
}

func parseHostPort(s string) (user, host string, port int) {
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user, s = s[:i], s[i+1:]
	}
	host = s
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i > 0 {
			host = s[1:i]
			if strings.HasPrefix(s[i+1:], ":") {
				port, _ = strconv.Atoi(s[i+2:])
			}
		}
	} else if i := strings.LastIndex(s, ":"); i > 0 && strings.Count(s, ":") == 1 {
		host = s[:i]
		port, _ = strconv.Atoi(s[i+1:])
	}
	return
}

func parseForward(typ, spec string) (config.Forward, bool) {
	f := config.Forward{ID: config.NewID(), Type: typ, AutoStart: true}
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return f, false
	}
	bind := fields[0]
	if i := strings.LastIndex(bind, ":"); i >= 0 {
		f.BindHost = bind[:i]
		bind = bind[i+1:]
	}
	p, err := strconv.Atoi(bind)
	if err != nil {
		return f, false
	}
	f.BindPort = p
	if typ != "dynamic" {
		if len(fields) < 2 {
			return f, false
		}
		_, h, port := parseHostPort(fields[1])
		f.DestHost, f.DestPort = h, port
	}
	f.Label = fmt.Sprintf("%s %s", typ, spec)
	return f, true
}

func parseSSHConfig(data []byte, dir string) ([]Item, error) {
	hosts := readSSHConfig(data, dir, 0)
	// Collect global defaults from "Host *" blocks.
	defaults := map[string][]string{}
	for _, h := range hosts {
		for _, p := range h.patterns {
			if p == "*" {
				for k, v := range h.opts {
					if _, ok := defaults[k]; !ok {
						defaults[k] = v
					}
				}
			}
		}
	}
	var out []Item
	for _, h := range hosts {
		for _, alias := range h.patterns {
			if strings.ContainsAny(alias, "*?!") {
				continue
			}
			get := func(k string) string {
				if v := h.opts[k]; len(v) > 0 {
					return v[0]
				}
				if v := defaults[k]; len(v) > 0 {
					return v[0]
				}
				return ""
			}
			s := config.NewSite()
			s.Name = alias
			s.Folder = "SSH config"
			s.Host = get("hostname")
			if s.Host == "" {
				s.Host = alias
			}
			s.Host = strings.ReplaceAll(s.Host, "%h", alias)
			s.User = get("user")
			if p, err := strconv.Atoi(get("port")); err == nil && p != 22 {
				s.Port = p
			}
			if id := get("identityfile"); id != "" {
				s.KeyFile = expandTilde(id)
			}
			if pc := get("proxycommand"); pc != "" && !strings.EqualFold(pc, "none") {
				s.Proxy.Type = "command"
				s.Proxy.Command = strings.NewReplacer("%h", "%host", "%p", "%port", "%r", "%user").Replace(pc)
			}
			if pj := get("proxyjump"); pj != "" && !strings.EqualFold(pj, "none") {
				for _, hop := range strings.Split(pj, ",") {
					u, hh, port := parseHostPort(strings.TrimSpace(hop))
					// a jump may reference another alias
					j := config.Jump{Host: hh, Port: port, User: u}
					for _, other := range hosts {
						for _, op := range other.patterns {
							if op == hh {
								if v := other.opts["hostname"]; len(v) > 0 {
									j.Host = v[0]
								}
								if v := other.opts["user"]; len(v) > 0 && j.User == "" {
									j.User = v[0]
								}
								if v := other.opts["port"]; len(v) > 0 && j.Port == 0 {
									j.Port, _ = strconv.Atoi(v[0])
								}
								if v := other.opts["identityfile"]; len(v) > 0 {
									j.KeyFile = expandTilde(v[0])
								}
							}
						}
					}
					if j.Port == 0 {
						j.Port = 22
					}
					s.Jumps = append(s.Jumps, j)
				}
			}
			for _, v := range h.opts["localforward"] {
				if f, ok := parseForward("local", v); ok {
					s.Forwards = append(s.Forwards, f)
				}
			}
			for _, v := range h.opts["remoteforward"] {
				if f, ok := parseForward("remote", v); ok {
					s.Forwards = append(s.Forwards, f)
				}
			}
			for _, v := range h.opts["dynamicforward"] {
				if f, ok := parseForward("dynamic", v); ok {
					s.Forwards = append(s.Forwards, f)
				}
			}
			if ka, err := strconv.Atoi(get("serveraliveinterval")); err == nil {
				s.Keepalive = ka
			}
			out = append(out, Item{Site: s})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no concrete Host entries found")
	}
	return out, nil
}
