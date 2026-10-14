package importer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"skiff/internal/config"
)

// Export renders sites as "json" (Skiff, no secrets), "winscp-ini",
// "ssh-config" or "urls".
func Export(sites []config.Site, format string) (string, error) {
	switch format {
	case "", "json":
		clean := make([]config.Site, len(sites))
		for i, s := range sites {
			s.Password, s.Passphrase, s.Proxy.Password, s.S3SessionToken = "", "", "", ""
			s.Jumps = append([]config.Jump(nil), s.Jumps...)
			for j := range s.Jumps {
				s.Jumps[j].Password = ""
			}
			clean[i] = s
		}
		b, err := json.MarshalIndent(map[string]any{"version": 1, "sites": clean}, "", "  ")
		return string(b), err
	case "urls":
		var sb strings.Builder
		for _, s := range sites {
			sb.WriteString(s.URL(false, ""))
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	case "ssh-config":
		var sb strings.Builder
		for _, s := range sites {
			if s.Protocol != config.ProtoSFTP && s.Protocol != config.ProtoSCP {
				continue
			}
			alias := strings.ReplaceAll(s.Title(), " ", "-")
			fmt.Fprintf(&sb, "Host %s\n    HostName %s\n", alias, s.Host)
			if s.User != "" {
				fmt.Fprintf(&sb, "    User %s\n", s.User)
			}
			if s.Port != 0 && s.Port != 22 {
				fmt.Fprintf(&sb, "    Port %d\n", s.Port)
			}
			if s.KeyFile != "" && !strings.Contains(s.KeyFile, "\n") {
				fmt.Fprintf(&sb, "    IdentityFile %s\n", s.KeyFile)
			}
			if len(s.Jumps) > 0 {
				var hops []string
				for _, j := range s.Jumps {
					h := j.Host
					if j.User != "" {
						h = j.User + "@" + h
					}
					if j.Port != 0 && j.Port != 22 {
						h = fmt.Sprintf("%s:%d", h, j.Port)
					}
					hops = append(hops, h)
				}
				fmt.Fprintf(&sb, "    ProxyJump %s\n", strings.Join(hops, ","))
			}
			for _, f := range s.Forwards {
				switch f.Type {
				case "local":
					fmt.Fprintf(&sb, "    LocalForward %d %s:%d\n", f.BindPort, f.DestHost, f.DestPort)
				case "remote":
					fmt.Fprintf(&sb, "    RemoteForward %d %s:%d\n", f.BindPort, f.DestHost, f.DestPort)
				case "dynamic":
					fmt.Fprintf(&sb, "    DynamicForward %d\n", f.BindPort)
				}
			}
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	case "winscp-ini":
		var sb strings.Builder
		for _, s := range sites {
			name := s.Title()
			if s.Folder != "" {
				name = s.Folder + "/" + name
			}
			esc := strings.NewReplacer("%", "%25", " ", "%20", "\\", "%5C", "[", "%5B", "]", "%5D", "=", "%3D").Replace(name)
			fmt.Fprintf(&sb, "[Sessions\\%s]\n", esc)
			fmt.Fprintf(&sb, "HostName=%s\n", s.Host)
			if s.Port != 0 {
				fmt.Fprintf(&sb, "PortNumber=%d\n", s.Port)
			}
			if s.User != "" {
				fmt.Fprintf(&sb, "UserName=%s\n", url.PathEscape(s.User))
			}
			proto, ftps := 2, 0
			switch s.Protocol {
			case config.ProtoSCP:
				proto = 0
			case config.ProtoFTP:
				proto = 5
				switch s.FTPS {
				case "implicit":
					ftps = 1
				case "explicit":
					ftps = 3
				}
			case config.ProtoWebDAV:
				proto = 6
				if s.TLS {
					ftps = 1
				}
			case config.ProtoS3:
				proto = 7
				if s.TLS {
					ftps = 1
				}
			}
			fmt.Fprintf(&sb, "FSProtocol=%d\n", proto)
			if ftps != 0 {
				fmt.Fprintf(&sb, "Ftps=%d\n", ftps)
			}
			if s.RemoteDir != "" {
				fmt.Fprintf(&sb, "RemoteDirectory=%s\n", url.PathEscape(s.RemoteDir))
			}
			if s.LocalDir != "" {
				fmt.Fprintf(&sb, "LocalDirectory=%s\n", url.PathEscape(s.LocalDir))
			}
			if s.KeyFile != "" && !strings.Contains(s.KeyFile, "\n") {
				fmt.Fprintf(&sb, "PublicKeyFile=%s\n", url.PathEscape(s.KeyFile))
			}
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("unknown export format %q", format)
}
