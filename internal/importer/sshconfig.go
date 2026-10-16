package importer

import (
	"errors"
	"fmt"
	"strings"

	"skiff/internal/config"
	"skiff/internal/sshconfig"
)

// parseSSHConfig turns the Host entries of an OpenSSH config file into
// sites, with the values ssh would use for each (Host * defaults, Match
// blocks, Include). data is the content of the file at path.
func parseSSHConfig(data []byte, path string) ([]Item, error) {
	cfg := sshconfig.New(path, "")
	cfg.UserData = data
	var out []Item
	for _, e := range cfg.Hosts() {
		h := cfg.Resolve(e.Alias, sshconfig.Query{})
		s := config.NewSite()
		s.Name = e.Alias
		s.Folder = "SSH config"
		s.Host = h.HostName()
		if h.Has("User") {
			s.User = h.User()
		}
		if p := h.Port(); p != 22 {
			s.Port = p
		}
		var notes []string
		if ids := h.IdentityFiles(); len(ids) > 0 {
			s.KeyFile = ids[0]
			if len(ids) > 1 {
				notes = append(notes, fmt.Sprintf("only the first of %d IdentityFile is used", len(ids)))
			}
		}
		if pc := h.Value("ProxyCommand"); pc != "" && !strings.EqualFold(pc, "none") {
			s.Proxy.Type = "command"
			// to Skiff's placeholders (%% is kept so that %%h stays literal)
			s.Proxy.Command = strings.NewReplacer("%h", "%host", "%p", "%port", "%r", h.User(), "%n", e.Alias, "%%", "%%").Replace(pc)
		}
		for _, hop := range h.ProxyJump() {
			// a jump may reference another alias
			jh := cfg.Resolve(hop.Host, sshconfig.Query{User: hop.User, Port: hop.Port})
			j := config.Jump{Host: jh.HostName(), Port: jh.Port(), User: hop.User}
			if j.User == "" && jh.Has("User") {
				j.User = jh.User()
			}
			if ids := jh.IdentityFiles(); len(ids) > 0 {
				j.KeyFile = ids[0]
			}
			s.Jumps = append(s.Jumps, j)
		}
		for _, f := range h.Forwards() {
			s.Forwards = append(s.Forwards, config.Forward{
				ID: config.NewID(), Type: f.Type, BindHost: f.BindHost, BindPort: f.BindPort,
				DestHost: f.DestHost, DestPort: f.DestPort, AutoStart: true, Label: f.Type + " " + f.Spec,
			})
		}
		if n, ok := h.Int("ServerAliveInterval"); ok {
			s.Keepalive = n
		}
		if n, ok := h.Int("ConnectTimeout"); ok && n > 0 {
			s.Timeout = n
		}
		switch strings.ToLower(h.Value("AddressFamily")) {
		case "inet":
			s.IPVersion = "4"
		case "inet6":
			s.IPVersion = "6"
		}
		out = append(out, Item{Site: s, Note: strings.Join(notes, "; ")})
	}
	if len(out) == 0 {
		return nil, errors.New("no concrete Host entries found")
	}
	return out, nil
}
