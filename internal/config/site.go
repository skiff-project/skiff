package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Protocols supported by Skiff.
const (
	ProtoSFTP   = "sftp"
	ProtoSCP    = "scp"
	ProtoFTP    = "ftp"
	ProtoWebDAV = "webdav"
	ProtoS3     = "s3"
)

// Proxy settings (password stored encrypted).
type Proxy struct {
	Type      string `json:"type,omitempty"` // "", socks4, socks5, http, telnet, command
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	User      string `json:"user,omitempty"`
	Password  string `json:"password,omitempty"`
	Command   string `json:"command,omitempty"`
	RemoteDNS bool   `json:"remoteDns,omitempty"`
}

// Jump is one SSH hop used to reach the server (ProxyJump / WinSCP tunnel).
type Jump struct {
	SiteID   string `json:"siteId,omitempty"` // reuse a saved SSH site
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	KeyFile  string `json:"keyFile,omitempty"`
	HostKey  string `json:"hostKey,omitempty"`
}

// Forward is a port forwarding rule started with the session.
type Forward struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "local", "remote", "dynamic"
	BindHost  string `json:"bindHost,omitempty"`
	BindPort  int    `json:"bindPort"`
	DestHost  string `json:"destHost,omitempty"`
	DestPort  int    `json:"destPort,omitempty"`
	AutoStart bool   `json:"autoStart,omitempty"`
	Label     string `json:"label,omitempty"`
}

// Site is a saved session (WinSCP "site").
type Site struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder,omitempty"`
	Color  string `json:"color,omitempty"`
	Notes  string `json:"notes,omitempty"`

	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"` // encrypted at rest

	// SSH (SFTP/SCP)
	KeyFile        string   `json:"keyFile,omitempty"`
	Passphrase     string   `json:"passphrase,omitempty"` // encrypted
	UseAgent       bool     `json:"useAgent"`
	KbdInteractive bool     `json:"kbdInteractive"`
	HostKey        string   `json:"hostKey,omitempty"`
	SftpServer     string   `json:"sftpServer,omitempty"`
	Shell          string   `json:"shell,omitempty"`
	ListCommand    string   `json:"listCommand,omitempty"`
	LegacyAlgos    bool     `json:"legacyAlgos,omitempty"`
	Ciphers        []string `json:"ciphers,omitempty"`
	KeyExchanges   []string `json:"kex,omitempty"`
	MACs           []string `json:"macs,omitempty"`
	HostKeyAlgos   []string `json:"hostKeyAlgos,omitempty"`

	// FTP
	FTPS           string `json:"ftps,omitempty"` // none, explicit, implicit
	FTPActive      bool   `json:"ftpActive,omitempty"`
	FTPActiveIP    string `json:"ftpActiveIp,omitempty"`
	FTPForcePasvIP string `json:"ftpForcePasvIp,omitempty"`
	FTPMLSD        string `json:"ftpMlsd,omitempty"`
	FTPListHidden  bool   `json:"ftpListHidden,omitempty"`
	FTPAccount     string `json:"ftpAccount,omitempty"`
	Encoding       string `json:"encoding,omitempty"`

	// WebDAV / S3 / FTPS
	TLS            bool   `json:"tls,omitempty"`
	TLSInsecure    bool   `json:"tlsInsecure,omitempty"`
	TLSFingerprint string `json:"tlsFingerprint,omitempty"`

	// S3
	S3Region       string `json:"s3Region,omitempty"`
	S3PathStyle    bool   `json:"s3PathStyle,omitempty"`
	S3SessionToken string `json:"s3SessionToken,omitempty"` // encrypted

	// Directories & environment
	RemoteDir       string `json:"remoteDir,omitempty"`
	LocalDir        string `json:"localDir,omitempty"`
	SyncBrowsing    bool   `json:"syncBrowsing,omitempty"`
	RecycleBin      string `json:"recycleBin,omitempty"`
	ResolveSymlinks bool   `json:"resolveSymlinks"`
	EOL             string `json:"eol,omitempty"` // "LF" or "CRLF"
	TimeOffset      int    `json:"timeOffset,omitempty"` // minutes added to listed times

	// Connection
	Timeout   int     `json:"timeout,omitempty"`   // seconds
	Keepalive int     `json:"keepalive,omitempty"` // seconds, 0 = off
	IPVersion string  `json:"ipVersion,omitempty"`
	Proxy     Proxy   `json:"proxy,omitempty"`
	Jumps     []Jump  `json:"jumps,omitempty"`
	Forwards  []Forward `json:"forwards,omitempty"`
	// Parallel is the number of connections for background transfers.
	Parallel int `json:"parallel,omitempty"`

	Created  time.Time `json:"created,omitempty"`
	Updated  time.Time `json:"updated,omitempty"`
	LastUsed time.Time `json:"lastUsed,omitempty"`
	UseCount int       `json:"useCount,omitempty"`
}

// NewID returns a random identifier.
func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// DefaultPort for a protocol.
func DefaultPort(s *Site) int {
	switch s.Protocol {
	case ProtoSFTP, ProtoSCP:
		return 22
	case ProtoFTP:
		if s.FTPS == "implicit" {
			return 990
		}
		return 21
	case ProtoWebDAV, ProtoS3:
		if s.TLS {
			return 443
		}
		return 80
	}
	return 0
}

// ApplyDefaults fills zero values with sensible defaults.
func (s *Site) ApplyDefaults() {
	if s.Protocol == "" {
		s.Protocol = ProtoSFTP
	}
	if s.Timeout == 0 {
		s.Timeout = 15
	}
	if s.FTPS == "" {
		s.FTPS = "none"
	}
}

// NewSite returns a site with defaults.
func NewSite() Site {
	s := Site{ID: NewID(), Protocol: ProtoSFTP, UseAgent: true, KbdInteractive: true, ResolveSymlinks: true, Keepalive: 30}
	s.ApplyDefaults()
	return s
}

// Title returns the display name.
func (s *Site) Title() string {
	if s.Name != "" {
		return s.Name
	}
	if s.User != "" {
		return s.User + "@" + s.Host
	}
	return s.Host
}

// EffectivePort returns the configured or default port.
func (s *Site) EffectivePort() int {
	if s.Port != 0 {
		return s.Port
	}
	return DefaultPort(s)
}

// URL renders a session URL (without the password unless withPassword).
func (s *Site) URL(withPassword bool, plainPassword string) string {
	scheme := s.Protocol
	switch {
	case s.Protocol == ProtoFTP && s.FTPS == "implicit":
		scheme = "ftps"
	case s.Protocol == ProtoFTP && s.FTPS == "explicit":
		scheme = "ftpes"
	case s.Protocol == ProtoWebDAV && s.TLS:
		scheme = "davs"
	case s.Protocol == ProtoWebDAV:
		scheme = "dav"
	}
	u := url.URL{Scheme: scheme, Host: s.Host}
	if s.Port != 0 && s.Port != DefaultPort(s) {
		u.Host = s.Host + ":" + strconv.Itoa(s.Port)
	}
	if s.User != "" {
		if withPassword && plainPassword != "" {
			u.User = url.UserPassword(s.User, plainPassword)
		} else {
			u.User = url.User(s.User)
		}
	}
	p := s.RemoteDir
	if p == "" {
		p = "/"
	}
	u.Path = p
	out := u.String()
	if s.HostKey != "" && (s.Protocol == ProtoSFTP || s.Protocol == ProtoSCP) {
		// WinSCP style: sftp://user;fingerprint=ssh-ed25519-xxx@host
		out = strings.Replace(out, "@", ";fingerprint="+url.QueryEscape(s.HostKey)+"@", 1)
	}
	return out
}

// ParseURL builds a site from a session URL such as
// sftp://user:pass@host:2222/path, ftps://, ftpes://, dav(s)://, s3://.
// It returns the plain password separately.
func ParseURL(raw string) (Site, string, error) {
	s := NewSite()
	if !strings.Contains(raw, "://") {
		raw = "sftp://" + raw
	}
	// WinSCP allows ";fingerprint=..." inside the userinfo part
	var fingerprint string
	if i := strings.Index(raw, ";fingerprint="); i >= 0 {
		end := strings.Index(raw[i:], "@")
		if end > 0 {
			fp, _ := url.QueryUnescape(raw[i+len(";fingerprint=") : i+end])
			fingerprint = fp
			raw = raw[:i] + raw[i+end:]
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return s, "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "sftp", "ssh":
		s.Protocol = ProtoSFTP
	case "scp":
		s.Protocol = ProtoSCP
	case "ftp":
		s.Protocol = ProtoFTP
	case "ftps":
		s.Protocol, s.FTPS = ProtoFTP, "implicit"
	case "ftpes":
		s.Protocol, s.FTPS = ProtoFTP, "explicit"
	case "dav", "webdav", "http":
		s.Protocol = ProtoWebDAV
	case "davs", "webdavs", "https":
		s.Protocol, s.TLS = ProtoWebDAV, true
	case "s3":
		s.Protocol, s.TLS = ProtoS3, true
	case "s3plain":
		s.Protocol = ProtoS3
	default:
		return s, "", fmt.Errorf("unknown protocol %q", u.Scheme)
	}
	s.Host = u.Hostname()
	if p := u.Port(); p != "" {
		s.Port, _ = strconv.Atoi(p)
	}
	var pass string
	if u.User != nil {
		s.User = u.User.Username()
		pass, _ = u.User.Password()
	}
	if u.Path != "" && u.Path != "/" {
		s.RemoteDir = u.Path
	}
	if fingerprint != "" {
		s.HostKey = fingerprint
	}
	if s.Protocol == ProtoS3 && s.Host == "" {
		s.Host = "s3.amazonaws.com"
	}
	s.ApplyDefaults()
	return s, pass, nil
}
