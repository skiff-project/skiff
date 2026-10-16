// Package core ties everything together: connections, the transfer queue,
// synchronisation, terminals, tunnels and editors. Both the HTTP API (GUI,
// web mode) and the script runner drive Skiff through this package.
package core

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"skiff/internal/config"
	"skiff/internal/prompt"
	"skiff/internal/sshx"
	"skiff/internal/transfer"
	"skiff/internal/vfs"
	"skiff/internal/vfs/localfs"
)

const LocalID = "local"

type Core struct {
	Store     *config.Store
	Bus       *Bus
	Broker    *Broker
	Prompter  prompt.Prompter
	HostKeys  *sshx.HostKeyStore
	Transfers *transfer.Manager
	Local     *localfs.FS

	mu    sync.Mutex
	conns map[string]*Conn

	terms    *terminals
	tunnels  *tunnels
	watchers *watchers
	edits    *externalEdits

	localWatch localWatch
	monitor    monitorState
	onShutdown []func()
	finds      sync.Map // id -> context.CancelFunc
}

// Options for New.
type Options struct {
	// LocalRoot jails the local file system (web mode).
	LocalRoot string
	// Prompter overrides the UI broker (script mode).
	Prompter prompt.Prompter
}

func New(store *config.Store, opts Options) *Core {
	bus := NewBus()
	c := &Core{
		Store:    store,
		Bus:      bus,
		Broker:   NewBroker(bus),
		HostKeys: sshx.NewHostKeyStore(store.KnownHostsPath()),
		Local:    &localfs.FS{Root: opts.LocalRoot},
		conns:    map[string]*Conn{},
	}
	c.Prompter = c.Broker
	if opts.Prompter != nil {
		c.Prompter = opts.Prompter
	}
	prefs := store.Prefs()
	c.Transfers = transfer.NewManager(c, bus, c.Prompter, prefs.Queue.MaxParallel)
	c.Transfers.ConnLogf = func(conn, format string, args ...any) {
		if cn, err := c.Conn(conn); err == nil {
			cn.log.Logf(format, args...)
		}
	}
	c.terms = newTerminals(c)
	c.tunnels = newTunnels(c)
	c.watchers = newWatchers(c)
	c.edits = newExternalEdits(c)
	c.OnShutdown(store.OnSitesChanged(func() { bus.Publish("sites.changed", nil) }))
	return c
}

// Conn is a live connection.
type Conn struct {
	ID     string
	SiteID string
	Title  string

	core *Core
	site config.Site // resolved, secrets in plain text (memory only)
	// forwards started with the session: the site's, else ssh_config's
	forwards []config.Forward
	// where the runtime of a container session runs
	container *containerHost
	// host, port and user really used (ssh_config may change them)
	host string
	port int
	user string

	mu           sync.Mutex
	fs           vfs.FS
	ssh          *sshx.Conn
	tunnel       *sshx.Conn
	caps         vfs.Caps
	home         string
	state        string
	err          string
	log          *Log
	idle         []vfs.FS
	forks        int // extra connections (channels) opened by acquire, in use or idle
	gen          int
	created      time.Time
	info         []vfs.InfoItem
	reconnecting bool
}

// ConnInfo is the UI view of a connection.
type ConnInfo struct {
	ID           string    `json:"id"`
	SiteID       string    `json:"siteId,omitempty"`
	Title        string    `json:"title"`
	Protocol     string    `json:"protocol"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	User         string    `json:"user"`
	State        string    `json:"state"`
	Error        string    `json:"error,omitempty"`
	Caps         vfs.Caps  `json:"caps"`
	Home         string    `json:"home"`
	RemoteDir    string    `json:"remoteDir,omitempty"`
	LocalDir     string    `json:"localDir,omitempty"`
	Color        string    `json:"color,omitempty"`
	SyncBrowsing bool      `json:"syncBrowsing,omitempty"`
	URL          string    `json:"url"`
	Created      time.Time `json:"created"`
	RecycleBin   string    `json:"recycleBin,omitempty"`
	EOL          string    `json:"eol,omitempty"`
}

func (c *Conn) Info() ConnInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	host, port, user := c.site.Host, c.site.EffectivePort(), c.site.User
	if c.host != "" {
		host, port, user = c.host, c.port, c.user
	}
	return ConnInfo{
		ID: c.ID, SiteID: c.SiteID, Title: c.Title, Protocol: c.site.Protocol, Host: host,
		Port: port, User: user, State: c.state, Error: c.err, Caps: c.caps,
		Home: c.home, RemoteDir: c.site.RemoteDir, LocalDir: c.site.LocalDir, Color: c.site.Color,
		SyncBrowsing: c.site.SyncBrowsing, URL: c.site.URL(false, ""), Created: c.created,
		RecycleBin: c.site.RecycleBin, EOL: c.site.EOL,
	}
}

func (c *Conn) Log() *Log { return c.log }

// SSH returns the SSH connection (SFTP/SCP), or nil.
func (c *Conn) SSH() *sshx.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ssh
}

func (c *Conn) setState(state, err string) {
	c.mu.Lock()
	c.state, c.err = state, err
	c.mu.Unlock()
	c.core.Bus.Publish("conn.update", c.Info())
}

// FS returns the main file system, reconnecting if the connection was lost.
func (c *Conn) FS(ctx context.Context) (vfs.FS, error) {
	c.mu.Lock()
	fs, state := c.fs, c.state
	c.mu.Unlock()
	if state == "lost" && c.core.Store.Prefs().AutoReconnect {
		if err := c.Reconnect(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
		fs = c.fs
		c.mu.Unlock()
	}
	if fs == nil {
		return nil, errors.New("not connected")
	}
	return fs, nil
}

// Connect opens a connection to a saved site, or an ad-hoc site when siteID is empty.
func (c *Core) Connect(ctx context.Context, siteID string, adhoc *config.Site, password string) (*Conn, error) {
	var site config.Site
	if siteID != "" {
		st, ok := c.Store.Site(siteID)
		if !ok {
			return nil, errors.New("site not found")
		}
		r, err := c.Store.Resolve(st)
		if err != nil {
			if errors.Is(err, config.ErrLocked) {
				return nil, errors.New("the master password is required to use saved passwords")
			}
			return nil, err
		}
		site = r
	} else if adhoc != nil {
		site = *adhoc
		site.ApplyDefaults()
	} else {
		return nil, errors.New("nothing to connect to")
	}
	if password != "" {
		site.Password = password
	}
	conn := &Conn{ID: randID(6), SiteID: siteID, Title: site.Title(), core: c, site: site, state: "connecting", created: time.Now()}
	prefs := c.Store.Prefs()
	conn.log = newLog(conn.ID, c.Bus, c.Store.LogDir(), prefs.Logging.ToFile, conn.Title, prefs.Logging.Level == "debug")
	c.mu.Lock()
	c.conns[conn.ID] = conn
	c.mu.Unlock()
	c.Bus.Publish("conn.update", conn.Info())

	if err := conn.open(ctx); err != nil {
		conn.log.Errorf("%v", err)
		c.mu.Lock()
		delete(c.conns, conn.ID)
		c.mu.Unlock()
		conn.log.Close()
		c.Bus.Publish("conn.closed", conn.ID)
		return nil, friendlyError(err)
	}
	if siteID != "" {
		c.Store.TouchSite(siteID)
	}
	// Auto-start tunnels.
	for _, f := range conn.forwards {
		if f.AutoStart && conn.ssh != nil {
			if _, err := c.StartTunnel(conn.ID, f); err != nil {
				conn.log.Errorf("Tunnel %s: %v", f.Label, err)
			}
		}
	}
	return conn, nil
}

func (conn *Conn) open(ctx context.Context) error {
	c := conn.core
	d, err := c.dial(ctx, conn.site, prompt.WithConn(c.Prompter, conn.ID), conn.log)
	if err != nil {
		return err
	}
	if conn.forwards == nil {
		conn.forwards = d.forwards
	}
	if d.host != "" {
		conn.mu.Lock()
		conn.host, conn.port, conn.user = d.host, d.port, d.user
		conn.mu.Unlock()
	}
	if d.password != "" {
		conn.site.Password = d.password
		if d.remember && conn.SiteID != "" {
			if st, ok := c.Store.Site(conn.SiteID); ok {
				pw := d.password
				c.Store.SaveSite(st, config.SecretInput{Password: &pw})
			}
		}
	}
	home, err := d.fs.Home(ctx)
	if err != nil {
		home = "/"
	}
	var info []vfs.InfoItem
	if in, ok := d.fs.(vfs.Infoer); ok {
		info = in.Info()
	}
	conn.mu.Lock()
	conn.fs, conn.ssh, conn.tunnel, conn.container = d.fs, d.ssh, d.tunnel, d.container
	conn.caps = d.fs.Caps()
	conn.home = home
	conn.info = info
	conn.gen++
	conn.state, conn.err = "connected", ""
	conn.mu.Unlock()
	conn.log.Logf("Connected (%s), home directory %s", conn.caps.Protocol, home)
	c.Bus.Publish("conn.update", conn.Info())
	if d.ssh != nil {
		go conn.watchDead(d.ssh)
	}
	return nil
}

func (conn *Conn) watchDead(s *sshx.Conn) {
	<-s.Dead
	conn.mu.Lock()
	same := conn.ssh == s && conn.state == "connected"
	conn.mu.Unlock()
	if same {
		conn.log.Warnf("Connection lost")
		conn.setState("lost", "Connection lost")
	}
}

// Reconnect re-establishes a lost connection with the same credentials.
func (conn *Conn) Reconnect(ctx context.Context) error {
	conn.mu.Lock()
	if conn.reconnecting {
		conn.mu.Unlock()
		// wait for the other reconnect
		for i := 0; i < 300; i++ {
			time.Sleep(100 * time.Millisecond)
			conn.mu.Lock()
			st := conn.state
			conn.mu.Unlock()
			if st == "connected" {
				return nil
			}
			if st != "connecting" {
				break
			}
		}
		return errors.New("reconnect failed")
	}
	conn.reconnecting = true
	old, oldTunnel, idle := conn.fs, conn.tunnel, conn.idle
	conn.idle, conn.forks = nil, 0
	conn.state = "connecting"
	conn.mu.Unlock()
	defer func() {
		conn.mu.Lock()
		conn.reconnecting = false
		conn.mu.Unlock()
	}()
	conn.core.Bus.Publish("conn.update", conn.Info())
	for _, f := range idle {
		if _, ok := f.(vfs.SharedFork); !ok {
			f.Close()
		}
	}
	if old != nil {
		old.Close()
	}
	if oldTunnel != nil {
		oldTunnel.Close()
	}
	conn.log.Logf("Reconnecting…")
	if err := conn.open(ctx); err != nil {
		conn.setState("lost", err.Error())
		return friendlyError(err)
	}
	return nil
}

// Close disconnects.
func (conn *Conn) Close() {
	c := conn.core
	c.Transfers.CancelConn(conn.ID)
	c.terms.closeConn(conn.ID)
	c.tunnels.closeConn(conn.ID)
	c.watchers.closeConn(conn.ID)
	c.edits.closeConn(conn.ID)
	conn.mu.Lock()
	fs, tunnel, idle := conn.fs, conn.tunnel, conn.idle
	conn.fs, conn.idle, conn.forks, conn.state = nil, nil, 0, "closed"
	conn.mu.Unlock()
	for _, f := range idle {
		if _, ok := f.(vfs.SharedFork); !ok {
			f.Close()
		}
	}
	if fs != nil {
		fs.Close()
	}
	if tunnel != nil {
		tunnel.Close()
	}
	conn.log.Logf("Disconnected")
	conn.log.Close()
	c.mu.Lock()
	delete(c.conns, conn.ID)
	c.mu.Unlock()
	c.Bus.Publish("conn.closed", conn.ID)
}

func (c *Core) Conn(id string) (*Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[id]
	if !ok {
		return nil, fmt.Errorf("connection %s not found", id)
	}
	return conn, nil
}

func (c *Core) Conns() []ConnInfo {
	c.mu.Lock()
	list := make([]*Conn, 0, len(c.conns))
	for _, conn := range c.conns {
		list = append(list, conn)
	}
	c.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].created.Before(list[j].created) })
	out := make([]ConnInfo, len(list))
	for i, conn := range list {
		out[i] = conn.Info()
	}
	return out
}

func (c *Core) Disconnect(id string) error {
	conn, err := c.Conn(id)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// FSFor returns the file system for a connection id ("local" included).
func (c *Core) FSFor(ctx context.Context, id string) (vfs.FS, error) {
	if id == LocalID || id == "" {
		return c.Local, nil
	}
	conn, err := c.Conn(id)
	if err != nil {
		return nil, err
	}
	return conn.FS(ctx)
}

// ---- transfer.Resolver ----

func (c *Core) Acquire(ctx context.Context, id string) (vfs.FS, func(), error) {
	if id == LocalID || id == "" {
		return c.Local, func() {}, nil
	}
	conn, err := c.Conn(id)
	if err != nil {
		return nil, nil, err
	}
	return conn.acquire(ctx)
}

// Extra connections opened for parallel transfers. Over SSH each one is an
// SFTP channel of the same connection, and OpenSSH allows 10 sessions per
// connection (MaxSessions): stay well below so that terminals and commands
// can always open their own channel. Past the limit, work shares the main
// connection, which handles concurrent requests anyway.
const (
	maxForks     = 5
	maxIdleForks = 2
)

func (conn *Conn) acquire(ctx context.Context) (vfs.FS, func(), error) {
	main, err := conn.FS(ctx)
	if err != nil {
		return nil, nil, err
	}
	conn.mu.Lock()
	gen := conn.gen
	if n := len(conn.idle); n > 0 {
		f := conn.idle[n-1]
		conn.idle = conn.idle[:n-1]
		conn.mu.Unlock()
		return f, conn.releaser(f, gen), nil
	}
	fk, ok := main.(vfs.Forker)
	if !ok || conn.forks >= maxForks {
		conn.mu.Unlock()
		return main, func() {}, nil
	}
	conn.forks++
	conn.mu.Unlock()
	f, err := fk.Fork(ctx)
	if err != nil {
		conn.mu.Lock()
		if conn.gen == gen {
			conn.forks--
		}
		conn.mu.Unlock()
		// fall back to the main connection
		conn.log.Warnf("Cannot open an extra connection: %v", err)
		return main, func() {}, nil
	}
	return f, conn.releaser(f, gen), nil
}

func (conn *Conn) releaser(f vfs.FS, gen int) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if _, shared := f.(vfs.SharedFork); shared {
				return
			}
			conn.mu.Lock()
			if conn.gen == gen && conn.state == "connected" && len(conn.idle) < maxIdleForks {
				conn.idle = append(conn.idle, f)
				conn.mu.Unlock()
				return
			}
			if conn.gen == gen {
				conn.forks--
			}
			conn.mu.Unlock()
			f.Close()
		})
	}
}

// closeIdle closes the idle extra connections, freeing server sessions.
func (conn *Conn) closeIdle() {
	conn.mu.Lock()
	idle := conn.idle
	conn.idle = nil
	conn.forks -= len(idle)
	conn.mu.Unlock()
	for _, f := range idle {
		if _, ok := f.(vfs.SharedFork); !ok {
			f.Close()
		}
	}
}

func (c *Core) Title(id string) string {
	if id == LocalID || id == "" {
		return "Local"
	}
	if conn, err := c.Conn(id); err == nil {
		return conn.Title
	}
	return id
}

func (c *Core) Changed(conn, dir string) {
	if (conn == LocalID || conn == "") && c.Local.Root == "" {
		dir = localfs.ExpandHome(dir)
	}
	c.Bus.Publish("fs.changed", map[string]string{"conn": conn, "dir": dir})
}

// EOL returns the line ending convention of a connection.
func (c *Core) EOL(id string) string {
	if id == LocalID || id == "" {
		if runtime.GOOS == "windows" {
			return "CRLF"
		}
		return "LF"
	}
	if conn, err := c.Conn(id); err == nil && conn.site.EOL != "" {
		return conn.site.EOL
	}
	return "LF"
}

// Shutdown closes everything.
// OnShutdown registers a function run by Shutdown (before the connections
// close).
func (c *Core) OnShutdown(f func()) {
	c.mu.Lock()
	c.onShutdown = append(c.onShutdown, f)
	c.mu.Unlock()
}

func (c *Core) Shutdown() {
	c.mu.Lock()
	hooks := c.onShutdown
	c.onShutdown = nil
	c.mu.Unlock()
	for _, f := range hooks {
		f()
	}
	c.Transfers.Close()
	c.closeLocalWatch()
	c.mu.Lock()
	list := make([]*Conn, 0, len(c.conns))
	for _, conn := range c.conns {
		list = append(list, conn)
	}
	c.mu.Unlock()
	for _, conn := range list {
		conn.Close()
	}
}
