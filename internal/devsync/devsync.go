// Package devsync keeps the settings of several devices in step through
// one encrypted file on a server of the user (SFTP, SCP, FTP, WebDAV or S3).
// The file is encrypted with a passphrase that never leaves the devices
// (Argon2id + XChaCha20-Poly1305): the server only stores noise. Each
// device merges the file with its own settings item by item — the newest
// version of a site or a key wins, deletions are propagated — then writes
// the result back.
package devsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"

	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/prompt"
	"skiff/internal/vfs"
)

// Config is how this device synchronizes (devices.json).
type Config struct {
	Enabled  bool   `json:"enabled"`
	SiteID   string `json:"siteId"`   // where the shared file is
	Path     string `json:"path"`     // relative to the home folder of the site, or absolute
	Interval int    `json:"interval"` // minutes between automatic syncs (0: only by hand and after changes)
	Device   string `json:"device"`   // name of this device
	// what is shared
	Sites    bool `json:"sites"`
	Keychain bool `json:"keychain"`
	Prefs    bool `json:"prefs"`
	HostKeys bool `json:"hostKeys"`
	// Passphrase is encrypted like the passwords; never sent to the UI.
	Passphrase string `json:"passphrase,omitempty"`
}

// State is the outcome of the last synchronizations.
type State struct {
	LastSync   time.Time `json:"lastSync,omitempty"`
	LastOK     time.Time `json:"lastOk,omitempty"`     // last successful sync
	LastStatus string    `json:"lastStatus,omitempty"` // ok, failed
	LastError  string    `json:"lastError,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	LastDevice string    `json:"lastDevice,omitempty"` // device that wrote the file before us
	// PrefsHash is the hash of the shared preferences after the last sync,
	// to tell which side changed them since.
	PrefsHash string  `json:"prefsHash,omitempty"`
	Running   bool    `json:"running"`
	Result    *Result `json:"result,omitempty"` // of the last successful sync
}

// Bundle is the content of the shared file.
type Bundle struct {
	Version   int                  `json:"version"`
	Device    string               `json:"device"`
	Written   time.Time            `json:"written"`
	Sites     []config.Site        `json:"sites,omitempty"` // secrets in clear (the file is encrypted)
	Folders   []string             `json:"folders,omitempty"`
	Keys      []config.KeychainKey `json:"keys,omitempty"`
	Prefs     json.RawMessage      `json:"prefs,omitempty"`
	PrefsHash string               `json:"prefsHash,omitempty"`
	HostKeys  []string             `json:"hostKeys,omitempty"` // lines of Skiff's known_hosts
	Deleted   map[string]time.Time `json:"deleted,omitempty"`
	// Storage is the site that keeps the file, on the device that wrote it.
	Storage string `json:"storage,omitempty"`
}

// Result says what a synchronization changed.
type Result struct {
	Uploaded     bool   `json:"uploaded"`
	Created      bool   `json:"created"`  // first sync: the file did not exist
	SitesIn      int    `json:"sitesIn"`  // added or updated here
	SitesOut     int    `json:"sitesOut"` // removed here
	KeysIn       int    `json:"keysIn"`
	KeysOut      int    `json:"keysOut"`
	PrefsIn      bool   `json:"prefsIn"`
	HostKeysIn   int    `json:"hostKeysIn"`
	RemoteDevice string `json:"remoteDevice,omitempty"`
}

var (
	ErrNoPassphrase = errors.New("no passphrase: set one in Preferences › Devices")
	ErrPassphrase   = errors.New("wrong passphrase, or the file was not written by Skiff")
	magic           = []byte("SKIFFSYNC1\n")
)

// ---- encryption ----

func deriveKey(pass string, salt []byte) []byte {
	return argon2.IDKey([]byte(pass), salt, 3, 64*1024, 2, chacha20poly1305.KeySize)
}

func seal(pass string, plain []byte) ([]byte, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	aead, err := chacha20poly1305.NewX(deriveKey(pass, salt))
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	out := append(append(append(append([]byte(nil), magic...), salt...), nonce...), aead.Seal(nil, nonce, plain, magic)...)
	return out, nil
}

func open(pass string, data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, magic) {
		return nil, ErrPassphrase
	}
	data = data[len(magic):]
	if len(data) < 16+chacha20poly1305.NonceSizeX {
		return nil, ErrPassphrase
	}
	salt, nonce, ct := data[:16], data[16:16+chacha20poly1305.NonceSizeX], data[16+chacha20poly1305.NonceSizeX:]
	aead, err := chacha20poly1305.NewX(deriveKey(pass, salt))
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, ct, magic)
	if err != nil {
		return nil, ErrPassphrase
	}
	return plain, nil
}

// ---- the syncer ----

// Syncer synchronizes this device.
type Syncer struct {
	cfg  *config.Store
	bus  Publisher
	mu   sync.Mutex // one sync at a time
	smu  sync.Mutex // conf and state
	conf Config
	st   State
	// HostKeysFile is Skiff's known_hosts (shared when HostKeys is set).
	HostKeysFile string
}

// Publisher emits events to the interface.
type Publisher interface {
	Publish(typ string, data any)
}

func New(cfg *config.Store, bus Publisher, hostKeysFile string) *Syncer {
	s := &Syncer{cfg: cfg, bus: bus, HostKeysFile: hostKeysFile}
	var file struct {
		Config Config `json:"config"`
		State  State  `json:"state"`
	}
	if b, err := os.ReadFile(s.path()); err == nil {
		json.Unmarshal(b, &file)
	}
	s.conf, s.st = file.Config, file.State
	if s.conf.Device == "" {
		s.conf.Device, _ = os.Hostname()
	}
	if s.conf.Path == "" {
		s.conf.Path = "skiff-sync.bin"
		s.conf.Sites, s.conf.Keychain, s.conf.Prefs, s.conf.HostKeys = true, true, true, true
		s.conf.Interval = 15
	}
	s.st.Running = false
	return s
}

func (s *Syncer) path() string { return filepath.Join(s.cfg.Dir, "devices.json") }

func (s *Syncer) saveLocked() error {
	b, _ := json.MarshalIndent(map[string]any{"config": s.conf, "state": s.st}, "", "  ")
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// View is the configuration for the interface (no passphrase).
type View struct {
	Config
	HasPassphrase bool  `json:"hasPassphrase"`
	State         State `json:"state"`
}

func (s *Syncer) View() View {
	s.smu.Lock()
	defer s.smu.Unlock()
	v := View{Config: s.conf, HasPassphrase: s.conf.Passphrase != "", State: s.st}
	v.Passphrase = ""
	return v
}

// SetConfig saves the configuration; passphrase nil keeps the current one.
func (s *Syncer) SetConfig(c Config, passphrase *string) error {
	if c.Enabled && c.SiteID == "" {
		return errors.New("choose the site that keeps the shared file")
	}
	if strings.TrimSpace(c.Path) == "" {
		c.Path = "skiff-sync.bin"
	}
	s.smu.Lock()
	defer s.smu.Unlock()
	c.Passphrase = s.conf.Passphrase
	if passphrase != nil {
		if len(*passphrase) < 8 {
			return errors.New("the passphrase needs at least 8 characters")
		}
		enc, err := s.cfg.Secrets.Encrypt(*passphrase)
		if err != nil {
			return err
		}
		c.Passphrase = enc
	}
	if c.Enabled && c.Passphrase == "" {
		return ErrNoPassphrase
	}
	s.conf = c
	err := s.saveLocked()
	s.publishLocked()
	return err
}

func (s *Syncer) publishLocked() {
	if s.bus != nil {
		v := View{Config: s.conf, HasPassphrase: s.conf.Passphrase != "", State: s.st}
		v.Passphrase = ""
		s.bus.Publish("devsync.update", v)
	}
}

func (s *Syncer) setState(f func(*State)) {
	s.smu.Lock()
	f(&s.st)
	s.saveLocked()
	s.publishLocked()
	s.smu.Unlock()
}

// ---- shared preferences ----

// sharedPrefs leaves out what belongs to this device.
func sharedPrefs(p config.Prefs) config.Prefs {
	p.LastTabs, p.UI, p.Web, p.SSH.ConfigFile = nil, nil, config.WebPrefs{}, ""
	return p
}

func prefsJSON(p config.Prefs) ([]byte, string) {
	b, _ := json.Marshal(sharedPrefs(p))
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

// ---- remote file ----

type remote struct {
	c    *core.Core
	conn string
	path string
}

func (s *Syncer) dial(ctx context.Context, conf Config) (*remote, func(), error) {
	c := core.New(s.cfg, core.Options{Prompter: prompt.Unattended})
	conn, err := c.Connect(ctx, conf.SiteID, nil, "")
	if err != nil {
		c.Shutdown()
		return nil, nil, err
	}
	p := conf.Path
	if !strings.HasPrefix(p, "/") {
		p = strings.TrimRight(conn.Info().Home, "/") + "/" + p
	}
	return &remote{c: c, conn: conn.ID, path: p}, c.Shutdown, nil
}

func (r *remote) read(ctx context.Context) ([]byte, error) {
	f, err := r.c.FSFor(ctx, r.conn)
	if err != nil {
		return nil, err
	}
	rc, err := f.Open(ctx, r.path, 0)
	if err != nil {
		if vfs.IsNotExist(err) {
			return nil, nil
		}
		if _, serr := f.Stat(ctx, r.path); vfs.IsNotExist(serr) {
			return nil, nil
		}
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 64<<20))
}

func (r *remote) write(ctx context.Context, data []byte) error {
	f, err := r.c.FSFor(ctx, r.conn)
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	w, err := f.Create(ctx, tmp, vfs.CreateOpts{Size: int64(len(data)), Mode: 0o600})
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := f.Rename(ctx, tmp, r.path); err != nil {
		f.Remove(ctx, r.path, false)
		if err := f.Rename(ctx, tmp, r.path); err != nil {
			return err
		}
	}
	return nil
}

// ---- merge ----

func newer(a, b time.Time) bool { return a.After(b) }

// Sync downloads the shared file, merges it with this device and writes the
// result back.
func (s *Syncer) Sync(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.smu.Lock()
	conf, st := s.conf, s.st
	s.smu.Unlock()
	if conf.SiteID == "" {
		return Result{}, errors.New("choose the site that keeps the shared file")
	}
	if conf.Passphrase == "" {
		return Result{}, ErrNoPassphrase
	}
	pass, err := s.cfg.Secrets.Decrypt(conf.Passphrase)
	if err != nil {
		return Result{}, err
	}
	s.setState(func(x *State) { x.Running = true })
	res, err := s.sync(ctx, conf, st, pass)
	s.setState(func(x *State) {
		x.Running, x.LastSync = false, time.Now()
		if err != nil {
			x.LastStatus, x.LastError = "failed", err.Error()
		} else {
			x.LastStatus, x.LastError, x.Summary, x.LastDevice, x.LastOK = "ok", "", summary(res), res.RemoteDevice, x.LastSync
			r := res
			x.Result = &r
		}
	})
	return res, err
}

func summary(r Result) string {
	if r.Created {
		return "Shared file created"
	}
	var parts []string
	if r.SitesIn+r.SitesOut > 0 {
		parts = append(parts, fmt.Sprintf("sites +%d −%d", r.SitesIn, r.SitesOut))
	}
	if r.KeysIn+r.KeysOut > 0 {
		parts = append(parts, fmt.Sprintf("keys +%d −%d", r.KeysIn, r.KeysOut))
	}
	if r.PrefsIn {
		parts = append(parts, "preferences")
	}
	if r.HostKeysIn > 0 {
		parts = append(parts, fmt.Sprintf("host keys +%d", r.HostKeysIn))
	}
	if len(parts) == 0 {
		if r.Uploaded {
			return "Changes sent"
		}
		return "Already up to date"
	}
	return "Received: " + strings.Join(parts, ", ")
}

func (s *Syncer) sync(ctx context.Context, conf Config, st State, pass string) (Result, error) {
	var res Result
	r, closeFn, err := s.dial(ctx, conf)
	if err != nil {
		return res, err
	}
	defer closeFn()
	data, err := r.read(ctx)
	if err != nil {
		return res, err
	}
	var in Bundle
	if data == nil {
		res.Created = true
	} else {
		plain, err := open(pass, data)
		if err != nil {
			return res, err
		}
		if err := json.Unmarshal(plain, &in); err != nil {
			return res, fmt.Errorf("shared file: %w", err)
		}
		res.RemoteDevice = in.Device
	}

	tombs := s.cfg.Tombstones()
	for k, t := range in.Deleted {
		if newer(t, tombs[k]) {
			tombs[k] = t
		}
	}
	out := Bundle{Version: 1, Device: conf.Device, Written: time.Now(), Deleted: tombs}
	changedHere := false

	if conf.Sites {
		local, folders, err := s.cfg.SitesInClear()
		if err != nil {
			return res, err
		}
		if st.LastOK.IsZero() {
			// first sync of this device: the storage site, created by hand
			// here, is the one already shared
			local, conf.SiteID = s.adoptStorageSite(local, in.Sites, in.Storage, conf.SiteID)
		}
		merged, in2, out2 := mergeSites(local, in.Sites, tombs)
		res.SitesIn, res.SitesOut = in2, out2
		mf := mergeFolders(folders, in.Folders, tombs)
		if in2+out2 > 0 || !sameStrings(mf, folders) {
			if err := s.cfg.ReplaceSites(merged, mf); err != nil {
				return res, err
			}
			changedHere = true
		}
		out.Sites, out.Folders = merged, mf
	}
	if conf.Keychain {
		local, err := s.cfg.KeysInClear()
		if err != nil {
			return res, err
		}
		merged, in2, out2 := mergeKeys(local, in.Keys, tombs)
		res.KeysIn, res.KeysOut = in2, out2
		if in2+out2 > 0 {
			if err := s.cfg.ReplaceKeys(merged); err != nil {
				return res, err
			}
		}
		out.Keys = merged
	}
	if conf.Prefs {
		localJSON, localHash := prefsJSON(s.cfg.Prefs())
		remoteChanged := in.PrefsHash != "" && in.PrefsHash != st.PrefsHash
		// a device that never synced takes the shared preferences
		localChanged := st.PrefsHash != "" && localHash != st.PrefsHash
		if remoteChanged && !localChanged && in.PrefsHash != localHash {
			var p config.Prefs
			if err := json.Unmarshal(in.Prefs, &p); err == nil {
				cur := s.cfg.Prefs()
				p.LastTabs, p.UI, p.Web, p.SSH.ConfigFile = cur.LastTabs, cur.UI, cur.Web, cur.SSH.ConfigFile
				if err := s.cfg.SetPrefs(p); err != nil {
					return res, err
				}
				res.PrefsIn, changedHere = true, true
				// as stored (saving normalizes them)
				localJSON, localHash = prefsJSON(s.cfg.Prefs())
			}
		}
		out.Prefs, out.PrefsHash = localJSON, localHash
	}
	if conf.HostKeys && s.HostKeysFile != "" {
		lines, added, err := mergeHostKeys(s.HostKeysFile, in.HostKeys)
		if err != nil {
			return res, err
		}
		res.HostKeysIn = added
		out.HostKeys = lines
	}
	if err := s.cfg.MergeTombstones(tombs); err != nil {
		return res, err
	}
	out.Storage = conf.SiteID // after a possible adoption

	// write back only when something differs from the file
	if res.Created || !sameBundle(in, out) {
		plain, _ := json.Marshal(out)
		sealed, err := seal(pass, plain)
		if err != nil {
			return res, err
		}
		if err := r.write(ctx, sealed); err != nil {
			return res, err
		}
		res.Uploaded = true
	}
	s.setState(func(x *State) { x.PrefsHash = out.PrefsHash })
	if changedHere && s.bus != nil {
		s.bus.Publish("sites.changed", nil)
		s.bus.Publish("prefs.changed", nil)
	}
	return res, nil
}

// adoptStorageSite replaces the local storage site by the shared site
// that points to the same place, so that it is not there twice.
func (s *Syncer) adoptStorageSite(local, remote []config.Site, shared, id string) ([]config.Site, string) {
	var mine *config.Site
	for i := range local {
		if local[i].ID == id {
			mine = &local[i]
		}
	}
	if mine == nil {
		return local, id
	}
	// the storage site of the writer first
	sort.SliceStable(remote, func(i, j int) bool { return remote[i].ID == shared && remote[j].ID != shared })
	for _, r := range remote {
		if r.ID != id && r.Protocol == mine.Protocol && strings.EqualFold(r.Host, mine.Host) && r.EffectivePort() == mine.EffectivePort() && r.User == mine.User {
			out := make([]config.Site, 0, len(local))
			for _, l := range local {
				if l.ID != id {
					out = append(out, l)
				}
			}
			s.smu.Lock()
			s.conf.SiteID = r.ID
			s.saveLocked()
			s.smu.Unlock()
			return out, r.ID
		}
	}
	return local, id
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameBundle compares the content (not who wrote it, nor when).
func sameBundle(a, b Bundle) bool {
	norm := func(x Bundle) []byte {
		// who wrote it, when and from which storage site do not count
		x.Device, x.Written, x.Version, x.Storage = "", time.Time{}, 0, ""
		if len(x.Deleted) == 0 {
			x.Deleted = nil
		}
		x.Sites = append([]config.Site(nil), x.Sites...)
		x.Keys = append([]config.KeychainKey(nil), x.Keys...)
		x.Folders = append([]string(nil), x.Folders...)
		x.HostKeys = append([]string(nil), x.HostKeys...)
		for i := range x.Sites {
			x.Sites[i].LastUsed, x.Sites[i].UseCount = time.Time{}, 0
		}
		sort.Slice(x.Sites, func(i, j int) bool { return x.Sites[i].ID < x.Sites[j].ID })
		sort.Slice(x.Keys, func(i, j int) bool { return x.Keys[i].Fingerprint < x.Keys[j].Fingerprint })
		sort.Strings(x.Folders)
		sort.Strings(x.HostKeys)
		var prefs any
		json.Unmarshal(x.Prefs, &prefs)
		x.Prefs = nil
		b, _ := json.Marshal(struct {
			B Bundle
			P any
		}{x, prefs})
		return b
	}
	return bytes.Equal(norm(a), norm(b))
}

// mergeSites keeps, for each site, the newest version; a deletion newer
// than both versions removes it. It counts the sites added or updated, and
// removed, on this device.
func mergeSites(local, remote []config.Site, tombs map[string]time.Time) (out []config.Site, in, gone int) {
	byID := map[string]config.Site{}
	for _, s := range remote {
		byID[s.ID] = s
	}
	seen := map[string]bool{}
	for _, l := range local {
		seen[l.ID] = true
		r, ok := byID[l.ID]
		pick := l
		if ok && newer(r.Updated, l.Updated) {
			pick = r
			if !siteEqual(r, l) {
				in++
			}
		}
		if t, dead := tombs["site:"+l.ID]; dead && !newer(pick.Updated, t) {
			gone++
			continue
		}
		out = append(out, pick)
	}
	for _, r := range remote {
		if seen[r.ID] {
			continue
		}
		if t, dead := tombs["site:"+r.ID]; dead && !newer(r.Updated, t) {
			continue
		}
		out = append(out, r)
		in++
	}
	return out, in, gone
}

func siteEqual(a, b config.Site) bool {
	a.LastUsed, a.UseCount, b.LastUsed, b.UseCount = time.Time{}, 0, time.Time{}, 0
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func mergeFolders(local, remote []string, tombs map[string]time.Time) []string {
	set := map[string]bool{}
	var out []string
	for _, f := range append(append([]string(nil), local...), remote...) {
		if set[f] {
			continue
		}
		set[f] = true
		if _, dead := tombs["folder:"+f]; dead {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// mergeKeys merges by fingerprint: the same key added on two devices is one.
func mergeKeys(local, remote []config.KeychainKey, tombs map[string]time.Time) (out []config.KeychainKey, in, gone int) {
	byFP := map[string]config.KeychainKey{}
	for _, k := range remote {
		byFP[k.Fingerprint] = k
	}
	seen := map[string]bool{}
	for _, l := range local {
		seen[l.Fingerprint] = true
		pick := l
		if r, ok := byFP[l.Fingerprint]; ok && newer(r.Updated, l.Updated) {
			if r.Name != l.Name {
				in++
			}
			r.ID = l.ID
			pick = r
		}
		if t, dead := tombs["key:"+l.Fingerprint]; dead && !newer(pick.Updated, t) {
			gone++
			continue
		}
		out = append(out, pick)
	}
	for _, r := range remote {
		if seen[r.Fingerprint] {
			continue
		}
		if t, dead := tombs["key:"+r.Fingerprint]; dead && !newer(r.Updated, t) {
			continue
		}
		out = append(out, r)
		in++
	}
	return out, in, gone
}

// mergeHostKeys adds the remote lines to Skiff's known_hosts and returns
// the merged lines and how many were added here.
func mergeHostKeys(path string, remote []string) ([]string, int, error) {
	var local []string
	if b, err := os.ReadFile(path); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(ln) != "" {
				local = append(local, ln)
			}
		}
	}
	have := map[string]bool{}
	for _, ln := range local {
		have[ln] = true
	}
	out := append([]string(nil), local...)
	added := 0
	for _, ln := range remote {
		if strings.TrimSpace(ln) != "" && !have[ln] {
			have[ln] = true
			out = append(out, ln)
			added++
		}
	}
	if added > 0 {
		os.MkdirAll(filepath.Dir(path), 0o700)
		if err := os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
			return nil, 0, err
		}
	}
	return out, added, nil
}

// ---- automatic synchronization ----

// Start syncs at start-up, every Interval minutes and shortly after local
// changes, while enabled.
func (s *Syncer) Start(changes <-chan struct{}) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		run := func() {
			if v := s.View(); v.Enabled && v.HasPassphrase {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				s.Sync(ctx)
				cancel()
			}
		}
		select {
		case <-time.After(3 * time.Second):
			run()
		case <-stop:
			return
		}
		last := time.Now()
		var debounce <-chan time.Time
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-changes:
				debounce = time.After(20 * time.Second)
			case <-debounce:
				debounce = nil
				run()
				last = time.Now()
			case <-tick.C:
				if iv := s.View().Interval; iv > 0 && time.Since(last) >= time.Duration(iv)*time.Minute {
					run()
					last = time.Now()
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}
