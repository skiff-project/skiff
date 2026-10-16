// Package importer migrates saved sessions from WinSCP (INI file or
// registry), PuTTY, FileZilla and OpenSSH config files, and exports Skiff
// sites to other formats.
package importer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"skiff/internal/config"
)

// Source describes an import source.
type Source struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"` // detected default location
	Available bool   `json:"available"`
	NeedsFile bool   `json:"needsFile"`
}

// Item is one importable site.
type Item struct {
	Site       config.Site `json:"site"`
	Password   string      `json:"password,omitempty"`
	Passphrase string      `json:"passphrase,omitempty"`
	Note       string      `json:"note,omitempty"`
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p != "" && exists(p) {
			return p
		}
	}
	return ""
}

func appData() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return d
	}
	return ""
}

// Sources lists the import sources and whether they were detected.
func Sources() []Source {
	var out []Source
	winscpIni := firstExisting(filepath.Join(appData(), "WinSCP.ini"), filepath.Join(home(), ".config", "WinSCP.ini"))
	out = append(out, Source{ID: "winscp-ini", Name: "WinSCP (WinSCP.ini)", Path: winscpIni, Available: true, NeedsFile: winscpIni == ""})
	if runtime.GOOS == "windows" {
		out = append(out, Source{ID: "winscp-registry", Name: "WinSCP (registry)", Available: winscpRegistryAvailable()})
		out = append(out, Source{ID: "putty", Name: "PuTTY", Available: puttyRegistryAvailable()})
	} else {
		p := filepath.Join(home(), ".putty", "sessions")
		out = append(out, Source{ID: "putty", Name: "PuTTY", Path: p, Available: exists(p)})
	}
	fz := firstExisting(filepath.Join(appData(), "FileZilla", "sitemanager.xml"), filepath.Join(home(), ".config", "filezilla", "sitemanager.xml"), filepath.Join(home(), ".filezilla", "sitemanager.xml"))
	out = append(out, Source{ID: "filezilla", Name: "FileZilla", Path: fz, Available: true, NeedsFile: fz == ""})
	sc := filepath.Join(home(), ".ssh", "config")
	out = append(out, Source{ID: "ssh-config", Name: "OpenSSH (~/.ssh/config)", Path: sc, Available: exists(sc), NeedsFile: !exists(sc)})
	out = append(out, Source{ID: "skiff-json", Name: "Skiff export (JSON)", Available: true, NeedsFile: true})
	out = append(out, Source{ID: "urls", Name: "Session URLs (one per line)", Available: true, NeedsFile: true})
	return out
}

// Load reads items from a source; data, when given, replaces reading path.
func Load(source, path string, data []byte) ([]Item, error) {
	read := func(def string) ([]byte, error) {
		if len(data) > 0 {
			return data, nil
		}
		p := path
		if p == "" {
			p = def
		}
		if p == "" {
			return nil, errors.New("no file selected")
		}
		return os.ReadFile(p)
	}
	var items []Item
	var err error
	switch source {
	case "winscp-ini":
		var b []byte
		def := ""
		for _, s := range Sources() {
			if s.ID == source {
				def = s.Path
			}
		}
		if b, err = read(def); err == nil {
			items, err = parseWinSCPIni(b)
		}
	case "winscp-registry":
		items, err = loadWinSCPRegistry()
	case "putty":
		if runtime.GOOS == "windows" && path == "" && len(data) == 0 {
			items, err = loadPuttyRegistry()
		} else {
			dir := path
			if dir == "" {
				dir = filepath.Join(home(), ".putty", "sessions")
			}
			items, err = loadPuttyDir(dir)
		}
	case "filezilla":
		def := ""
		for _, s := range Sources() {
			if s.ID == source {
				def = s.Path
			}
		}
		var b []byte
		if b, err = read(def); err == nil {
			items, err = parseFileZilla(b)
		}
	case "ssh-config":
		var b []byte
		p := path
		if p == "" {
			p = filepath.Join(home(), ".ssh", "config")
		}
		if b, err = read(p); err == nil {
			items, err = parseSSHConfig(b, p)
		}
	case "skiff-json":
		var b []byte
		if b, err = read(""); err == nil {
			var sites []config.Site
			if err = json.Unmarshal(b, &sites); err != nil {
				var wrapped struct {
					Sites []config.Site `json:"sites"`
				}
				if err2 := json.Unmarshal(b, &wrapped); err2 == nil {
					sites, err = wrapped.Sites, nil
				}
			}
			for _, s := range sites {
				s.Password, s.Passphrase = "", ""
				items = append(items, Item{Site: s})
			}
		}
	case "urls":
		var b []byte
		if b, err = read(""); err == nil {
			sc := bufio.NewScanner(strings.NewReader(string(b)))
			for sc.Scan() {
				ln := strings.TrimSpace(sc.Text())
				if ln == "" || strings.HasPrefix(ln, "#") {
					continue
				}
				s, pw, perr := config.ParseURL(ln)
				if perr != nil {
					continue
				}
				items = append(items, Item{Site: s, Password: pw})
			}
		}
	default:
		return nil, fmt.Errorf("unknown import source %q", source)
	}
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Site.ID == "" {
			items[i].Site.ID = config.NewID()
		}
		items[i].Site.ApplyDefaults()
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Site, items[j].Site
		if a.Folder != b.Folder {
			return a.Folder < b.Folder
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if items == nil {
		items = []Item{}
	}
	return items, nil
}

// ---- WinSCP ---------------------------------------------------------------

// winscpDecrypt reverses WinSCP's password obfuscation (no master password).
func winscpDecrypt(host, user, enc string) (string, bool) {
	const magic, flag = 0xA3, 0xFF
	nibbles := make([]int, 0, len(enc))
	for _, c := range enc {
		v, err := strconv.ParseUint(string(c), 16, 8)
		if err != nil {
			return "", false
		}
		nibbles = append(nibbles, int(v))
	}
	next := func() (int, bool) {
		if len(nibbles) < 2 {
			return 0, false
		}
		v := ^(((nibbles[0] << 4) + nibbles[1]) ^ magic) & 0xFF
		nibbles = nibbles[2:]
		return v, true
	}
	f, ok := next()
	if !ok {
		return "", false
	}
	var length int
	if f == flag {
		if _, ok = next(); !ok {
			return "", false
		}
		if length, ok = next(); !ok {
			return "", false
		}
	} else {
		length = f
	}
	skip, ok := next()
	if !ok {
		return "", false
	}
	if skip*2 > len(nibbles) {
		return "", false
	}
	nibbles = nibbles[skip*2:]
	var sb strings.Builder
	for i := 0; i < length; i++ {
		v, ok := next()
		if !ok {
			return "", false
		}
		sb.WriteByte(byte(v))
	}
	pw := sb.String()
	if f == flag {
		key := user + host
		if !strings.HasPrefix(pw, key) {
			return "", false
		}
		pw = pw[len(key):]
	}
	return pw, true
}

func winscpUnescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// winscpSession converts the key/value pairs of one stored session.
func winscpSession(name string, kv map[string]string) (Item, bool) {
	name = winscpUnescape(name)
	if name == "Default Settings" || strings.HasSuffix(name, "/Default Settings") {
		return Item{}, false
	}
	get := func(k string) string { return winscpUnescape(kv[k]) }
	num := func(k string, def int) int {
		if v, err := strconv.Atoi(kv[k]); err == nil {
			return v
		}
		return def
	}
	if get("IsWorkspace") == "1" {
		return Item{}, false
	}
	s := config.NewSite()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		s.Folder, s.Name = name[:i], name[i+1:]
	} else {
		s.Name = name
	}
	s.Host = get("HostName")
	s.User = get("UserName")
	s.Port = num("PortNumber", 0)
	ftps := num("Ftps", 0)
	switch num("FSProtocol", 2) {
	case 0:
		s.Protocol = config.ProtoSCP
	case 5:
		s.Protocol = config.ProtoFTP
		switch ftps {
		case 1:
			s.FTPS = "implicit"
		case 2, 3:
			s.FTPS = "explicit"
		}
	case 6:
		s.Protocol = config.ProtoWebDAV
		s.TLS = ftps != 0
	case 7:
		s.Protocol = config.ProtoS3
		s.TLS = ftps != 0 || kv["Ftps"] == ""
		s.S3Region = get("S3DefaultRegion")
		s.S3PathStyle = num("S3UrlStyle", 0) == 1
	default:
		s.Protocol = config.ProtoSFTP
	}
	if s.Port == config.DefaultPort(&s) {
		s.Port = 0
	}
	s.KeyFile = get("PublicKeyFile")
	s.RemoteDir = get("RemoteDirectory")
	s.LocalDir = get("LocalDirectory")
	s.Notes = get("Note")
	if v := get("SftpServer"); v != "" && v != "Default" {
		s.SftpServer = v
	}
	if v := get("Shell"); v != "" {
		s.Shell = v
	}
	if num("FtpPasvMode", 1) == 0 {
		s.FTPActive = true
	}
	switch num("ProxyMethod", 0) {
	case 1:
		s.Proxy.Type = "socks4"
	case 2:
		s.Proxy.Type = "socks5"
	case 3:
		s.Proxy.Type = "http"
	case 4:
		s.Proxy.Type = "telnet"
		s.Proxy.Command = get("ProxyTelnetCommand")
	case 5:
		s.Proxy.Type = "command"
		s.Proxy.Command = get("ProxyLocalCommand")
	}
	if s.Proxy.Type != "" {
		s.Proxy.Host = get("ProxyHost")
		s.Proxy.Port = num("ProxyPort", 0)
		s.Proxy.User = get("ProxyUsername")
	}
	if num("Tunnel", 0) == 1 {
		j := config.Jump{Host: get("TunnelHostName"), Port: num("TunnelPortNumber", 22), User: get("TunnelUserName"), KeyFile: get("TunnelPublicKeyFile")}
		s.Jumps = append(s.Jumps, j)
	}
	if v := kv["Color"]; v != "" {
		if c, err := strconv.Atoi(v); err == nil && c != 0 {
			// WinSCP stores a BGR TColor
			s.Color = fmt.Sprintf("#%02x%02x%02x", c&0xFF, (c>>8)&0xFF, (c>>16)&0xFF)
		}
	}
	it := Item{Site: s}
	if enc := kv["Password"]; enc != "" {
		if pw, ok := winscpDecrypt(s.Host, s.User, enc); ok {
			it.Password = pw
		} else {
			it.Note = "password is protected by a WinSCP master password and was not imported"
		}
	}
	return it, s.Host != ""
}

func parseWinSCPIni(data []byte) ([]Item, error) {
	sessions := map[string]map[string]string{}
	var order []string
	var cur map[string]string
	text := strings.TrimPrefix(string(data), "\uFEFF")
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" || strings.HasPrefix(ln, ";") {
			continue
		}
		if strings.HasPrefix(ln, "[") && strings.HasSuffix(ln, "]") {
			sec := ln[1 : len(ln)-1]
			cur = nil
			if strings.HasPrefix(sec, `Sessions\`) {
				name := strings.TrimPrefix(sec, `Sessions\`)
				cur = map[string]string{}
				sessions[name] = cur
				order = append(order, name)
			}
			continue
		}
		if cur == nil {
			continue
		}
		if i := strings.IndexByte(ln, '='); i > 0 {
			cur[ln[:i]] = ln[i+1:]
		}
	}
	if len(order) == 0 {
		return nil, errors.New("no WinSCP sessions found in this file")
	}
	var out []Item
	for _, name := range order {
		if it, ok := winscpSession(name, sessions[name]); ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// ---- PuTTY -----------------------------------------------------------------

func puttySession(name string, kv map[string]string) (Item, bool) {
	if name == "Default Settings" {
		return Item{}, false
	}
	if p := kv["Protocol"]; p != "" && p != "ssh" {
		return Item{}, false
	}
	s := config.NewSite()
	s.Name = name
	host := kv["HostName"]
	if i := strings.LastIndex(host, "@"); i >= 0 {
		s.User, host = host[:i], host[i+1:]
	}
	s.Host = host
	if u := kv["UserName"]; u != "" {
		s.User = u
	}
	if p, err := strconv.Atoi(kv["PortNumber"]); err == nil && p != 22 {
		s.Port = p
	}
	s.KeyFile = kv["PublicKeyFile"]
	switch kv["ProxyMethod"] {
	case "1":
		s.Proxy.Type = "socks4"
	case "2":
		s.Proxy.Type = "socks5"
	case "3":
		s.Proxy.Type = "http"
	case "4":
		s.Proxy.Type = "telnet"
	case "5":
		s.Proxy.Type = "command"
		s.Proxy.Command = kv["ProxyTelnetCommand"]
	}
	if s.Proxy.Type != "" {
		s.Proxy.Host = kv["ProxyHost"]
		s.Proxy.Port, _ = strconv.Atoi(kv["ProxyPort"])
		s.Proxy.User = kv["ProxyUsername"]
	}
	return Item{Site: s}, s.Host != ""
}

func loadPuttyDir(dir string) ([]Item, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		kv := map[string]string{}
		for _, ln := range strings.Split(string(data), "\n") {
			if i := strings.IndexByte(ln, '='); i > 0 {
				kv[ln[:i]] = strings.TrimRight(ln[i+1:], "\r")
			}
		}
		if it, ok := puttySession(winscpUnescape(e.Name()), kv); ok {
			out = append(out, it)
		}
	}
	return out, nil
}
