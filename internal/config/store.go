package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the root of all persisted state.
type Store struct {
	Dir      string
	Portable bool
	Secrets  *Secrets

	mu      sync.RWMutex
	sites   sitesFile
	prefs   Prefs
	certs   map[string][]string // host -> trusted SHA-256 fingerprints
	keys    []KeychainKey
	tombs   map[string]time.Time // deletions, for the sync between devices
	onSites map[int]func()
	nextFn  int
}

type sitesFile struct {
	Version int      `json:"version"`
	Folders []string `json:"folders"`
	Sites   []Site   `json:"sites"`
}

// Open loads (or initialises) the configuration in dir.
func Open(dir string, portable bool) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := checkFormat(dir); err != nil {
		return nil, err
	}
	sec, err := newSecrets(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{Dir: dir, Portable: portable, Secrets: sec, certs: map[string][]string{}}
	s.sites.Version = 1
	if data, err := os.ReadFile(s.path("sites.json")); err == nil {
		if err := json.Unmarshal(data, &s.sites); err != nil {
			return nil, err
		}
	}
	for i := range s.sites.Sites {
		s.sites.Sites[i].ApplyDefaults()
	}
	s.prefs = DefaultPrefs()
	if data, err := os.ReadFile(s.path("settings.json")); err == nil {
		json.Unmarshal(data, &s.prefs)
	}
	s.prefs.normalize()
	if data, err := os.ReadFile(s.path("trusted_certs.json")); err == nil {
		json.Unmarshal(data, &s.certs)
	}
	s.loadKeychain()
	s.loadTombstones()
	return s, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// KnownHostsPath is Skiff's writable known_hosts.
func (s *Store) KnownHostsPath() string { return s.path("known_hosts") }

// LogDir is where session logs are written.
func (s *Store) LogDir() string { return s.path("logs") }

// OnSitesChanged calls fn after each change of the sites; the returned
// function stops it.
func (s *Store) OnSitesChanged(fn func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onSites == nil {
		s.onSites = map[int]func(){}
	}
	s.nextFn++
	id := s.nextFn
	s.onSites[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.onSites, id)
		s.mu.Unlock()
	}
}

func (s *Store) saveSitesLocked() error {
	sort.SliceStable(s.sites.Sites, func(i, j int) bool {
		a, b := s.sites.Sites[i], s.sites.Sites[j]
		if a.Folder != b.Folder {
			return strings.ToLower(a.Folder) < strings.ToLower(b.Folder)
		}
		return strings.ToLower(a.Title()) < strings.ToLower(b.Title())
	})
	data, err := json.MarshalIndent(s.sites, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path("sites.json"), data, 0o600); err != nil {
		return err
	}
	for _, fn := range s.onSites {
		go fn()
	}
	return nil
}

// Sites returns a copy of all sites (secrets still encrypted).
func (s *Store) Sites() []Site {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Site, len(s.sites.Sites))
	copy(out, s.sites.Sites)
	return out
}

// Folders returns explicit folders plus the ones implied by sites.
func (s *Store) Folders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := map[string]bool{}
	for _, f := range s.sites.Folders {
		set[f] = true
	}
	for _, st := range s.sites.Sites {
		f := st.Folder
		for f != "" {
			set[f] = true
			i := strings.LastIndex(f, "/")
			if i < 0 {
				break
			}
			f = f[:i]
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// Site returns one site by id.
func (s *Store) Site(id string) (Site, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, st := range s.sites.Sites {
		if st.ID == id {
			return st, true
		}
	}
	return Site{}, false
}

// SecretInput carries secret updates: nil = keep, "" = clear, value = set.
type SecretInput struct {
	Password       *string         `json:"password,omitempty"`
	Passphrase     *string         `json:"passphrase,omitempty"`
	ProxyPassword  *string         `json:"proxyPassword,omitempty"`
	S3SessionToken *string         `json:"s3SessionToken,omitempty"`
	JumpPasswords  map[int]*string `json:"jumpPasswords,omitempty"`
}

// SaveSite creates or updates a site. Secret fields of in are ignored;
// secrets come from sec.
func (s *Store) SaveSite(in Site, sec SecretInput) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var old *Site
	idx := -1
	for i := range s.sites.Sites {
		if s.sites.Sites[i].ID == in.ID && in.ID != "" {
			old = &s.sites.Sites[i]
			idx = i
			break
		}
	}
	now := time.Now()
	if old == nil {
		if in.ID == "" {
			in.ID = NewID()
		}
		in.Created = now
		in.Password, in.Passphrase, in.Proxy.Password, in.S3SessionToken = "", "", "", ""
		for i := range in.Jumps {
			in.Jumps[i].Password = ""
		}
	} else {
		in.Created = old.Created
		in.LastUsed = old.LastUsed
		in.UseCount = old.UseCount
		in.Password, in.Passphrase, in.Proxy.Password, in.S3SessionToken = old.Password, old.Passphrase, old.Proxy.Password, old.S3SessionToken
		for i := range in.Jumps {
			in.Jumps[i].Password = ""
			if i < len(old.Jumps) && old.Jumps[i].Host == in.Jumps[i].Host && old.Jumps[i].User == in.Jumps[i].User {
				in.Jumps[i].Password = old.Jumps[i].Password
			}
		}
	}
	in.Updated = now
	set := func(dst *string, v *string) error {
		if v == nil {
			return nil
		}
		enc, err := s.Secrets.Encrypt(*v)
		if err != nil {
			return err
		}
		*dst = enc
		return nil
	}
	if err := set(&in.Password, sec.Password); err != nil {
		return in, err
	}
	if err := set(&in.Passphrase, sec.Passphrase); err != nil {
		return in, err
	}
	if err := set(&in.Proxy.Password, sec.ProxyPassword); err != nil {
		return in, err
	}
	if err := set(&in.S3SessionToken, sec.S3SessionToken); err != nil {
		return in, err
	}
	for i, v := range sec.JumpPasswords {
		if i >= 0 && i < len(in.Jumps) {
			if err := set(&in.Jumps[i].Password, v); err != nil {
				return in, err
			}
		}
	}
	in.ApplyDefaults()
	if idx >= 0 {
		s.sites.Sites[idx] = in
	} else {
		s.sites.Sites = append(s.sites.Sites, in)
	}
	return in, s.saveSitesLocked()
}

// ImportSite stores a site whose secrets are given in plain text.
func (s *Store) ImportSite(in Site, password, passphrase string) (Site, error) {
	in.ID = ""
	sec := SecretInput{}
	if password != "" {
		sec.Password = &password
	}
	if passphrase != "" {
		sec.Passphrase = &passphrase
	}
	return s.SaveSite(in, sec)
}

func (s *Store) DeleteSites(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	del := map[string]bool{}
	for _, id := range ids {
		del[id] = true
	}
	out := s.sites.Sites[:0]
	var gone []string
	for _, st := range s.sites.Sites {
		if !del[st.ID] {
			out = append(out, st)
		} else {
			gone = append(gone, "site:"+st.ID)
		}
	}
	s.sites.Sites = out
	s.buryLocked(gone...)
	return s.saveSitesLocked()
}

// MoveSites changes the folder of the given sites.
func (s *Store) MoveSites(ids []string, folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for i := range s.sites.Sites {
		if set[s.sites.Sites[i].ID] {
			s.sites.Sites[i].Folder = folder
			s.sites.Sites[i].Updated = time.Now()
		}
	}
	return s.saveSitesLocked()
}

// DuplicateSite copies a site (including encrypted secrets).
// DuplicateSite copies a site (secrets included). name is the copy's name;
// empty means "<title> (copy)".
func (s *Store) DuplicateSite(id, name string) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.sites.Sites {
		if st.ID == id {
			c := st
			c.ID = NewID()
			c.Name = name
			if c.Name == "" {
				c.Name = st.Title() + " (copy)"
			}
			c.Created, c.Updated, c.LastUsed, c.UseCount = time.Now(), time.Now(), time.Time{}, 0
			c.Jumps = append([]Jump(nil), st.Jumps...)
			c.Forwards = append([]Forward(nil), st.Forwards...)
			s.sites.Sites = append(s.sites.Sites, c)
			return c, s.saveSitesLocked()
		}
	}
	return Site{}, errors.New("site not found")
}

// AddFolder registers an (empty) folder.
func (s *Store) AddFolder(name string) error {
	name = strings.Trim(name, "/ ")
	if name == "" {
		return errors.New("empty folder name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.sites.Folders {
		if f == name {
			return nil
		}
	}
	s.sites.Folders = append(s.sites.Folders, name)
	return s.saveSitesLocked()
}

// RenameFolder renames a folder and its subfolders.
func (s *Store) RenameFolder(from, to string) error {
	to = strings.Trim(to, "/ ")
	s.mu.Lock()
	defer s.mu.Unlock()
	re := func(f string) string {
		if f == from {
			return to
		}
		if strings.HasPrefix(f, from+"/") {
			return to + f[len(from):]
		}
		return f
	}
	var gone []string
	for i := range s.sites.Folders {
		if n := re(s.sites.Folders[i]); n != s.sites.Folders[i] {
			gone = append(gone, "folder:"+s.sites.Folders[i])
			s.sites.Folders[i] = n
		}
	}
	for i := range s.sites.Sites {
		if n := re(s.sites.Sites[i].Folder); n != s.sites.Sites[i].Folder {
			s.sites.Sites[i].Folder, s.sites.Sites[i].Updated = n, time.Now()
		}
	}
	s.buryLocked(gone...)
	return s.saveSitesLocked()
}

// DeleteFolder removes a folder; its sites are deleted when withSites,
// otherwise moved to the parent folder.
func (s *Store) DeleteFolder(name string, withSites bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	parent := ""
	if i := strings.LastIndex(name, "/"); i >= 0 {
		parent = name[:i]
	}
	in := func(f string) bool { return f == name || strings.HasPrefix(f, name+"/") }
	var folders, gone []string
	for _, f := range s.sites.Folders {
		if !in(f) {
			folders = append(folders, f)
		} else {
			gone = append(gone, "folder:"+f)
		}
	}
	s.sites.Folders = folders
	out := s.sites.Sites[:0]
	for _, st := range s.sites.Sites {
		if in(st.Folder) {
			if withSites {
				gone = append(gone, "site:"+st.ID)
				continue
			}
			st.Folder = parent
			st.Updated = time.Now()
		}
		out = append(out, st)
	}
	s.buryLocked(gone...)
	s.sites.Sites = out
	return s.saveSitesLocked()
}

// TouchSite records a successful connection.
func (s *Store) TouchSite(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sites.Sites {
		if s.sites.Sites[i].ID == id {
			s.sites.Sites[i].LastUsed = time.Now()
			s.sites.Sites[i].UseCount++
			s.saveSitesLocked()
			return
		}
	}
}

// Resolve returns the site with secrets decrypted.
func (s *Store) Resolve(st Site) (Site, error) {
	var err error
	dec := func(v *string) {
		if err != nil || *v == "" {
			return
		}
		*v, err = s.Secrets.Decrypt(*v)
	}
	dec(&st.Password)
	dec(&st.Passphrase)
	dec(&st.Proxy.Password)
	dec(&st.S3SessionToken)
	jumps := make([]Jump, len(st.Jumps))
	copy(jumps, st.Jumps)
	for i := range jumps {
		dec(&jumps[i].Password)
	}
	st.Jumps = jumps
	return st, err
}

// ReencryptAll is used when the master password changes.
func (s *Store) ReencryptAll(decrypt func(string) (string, error), encrypt func(string) (string, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites := make([]Site, len(s.sites.Sites))
	copy(sites, s.sites.Sites)
	conv := func(v *string) error {
		if *v == "" {
			return nil
		}
		p, err := decrypt(*v)
		if err != nil {
			return err
		}
		*v, err = encrypt(p)
		return err
	}
	for i := range sites {
		st := &sites[i]
		for _, v := range []*string{&st.Password, &st.Passphrase, &st.Proxy.Password, &st.S3SessionToken} {
			if err := conv(v); err != nil {
				return err
			}
		}
		st.Jumps = append([]Jump(nil), st.Jumps...)
		for j := range st.Jumps {
			if err := conv(&st.Jumps[j].Password); err != nil {
				return err
			}
		}
	}
	keys := append([]KeychainKey(nil), s.keys...)
	for i := range keys {
		if err := conv(&keys[i].Private); err != nil {
			return err
		}
	}
	s.sites.Sites = sites
	if err := s.saveSitesLocked(); err != nil {
		return err
	}
	s.keys = keys
	return s.saveKeychainLocked()
}

// SetMasterPassword changes the master password and re-encrypts secrets.
func (s *Store) SetMasterPassword(old, newPassword string) error {
	return s.Secrets.SetMaster(old, newPassword, s.ReencryptAll)
}

// ---- trusted certificates (netx.CertStore) ----

func (s *Store) IsTrusted(host, fp string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, f := range s.certs[host] {
		if f == fp {
			return true
		}
	}
	return false
}

func (s *Store) Trust(host, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certs[host] = append(s.certs[host], fp)
	data, _ := json.MarshalIndent(s.certs, "", "  ")
	return writeFileAtomic(s.path("trusted_certs.json"), data, 0o600)
}

// ---- preferences ----

func (s *Store) Prefs() Prefs {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefs.clone()
}

func (s *Store) SetPrefs(p Prefs) error {
	p.normalize()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefs = p
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path("settings.json"), data, 0o600)
}

// SiteView is what the UI sees: secrets replaced by presence flags.
type SiteView struct {
	Site
	HasPassword       bool   `json:"hasPassword"`
	HasPassphrase     bool   `json:"hasPassphrase"`
	HasProxyPassword  bool   `json:"hasProxyPassword"`
	HasS3SessionToken bool   `json:"hasS3SessionToken"`
	JumpHasPassword   []bool `json:"jumpHasPassword,omitempty"`
}

func View(st Site) SiteView {
	v := SiteView{Site: st,
		HasPassword: st.Password != "", HasPassphrase: st.Passphrase != "",
		HasProxyPassword: st.Proxy.Password != "", HasS3SessionToken: st.S3SessionToken != ""}
	v.Site.Password, v.Site.Passphrase, v.Site.Proxy.Password, v.Site.S3SessionToken = "", "", "", ""
	v.Site.Jumps = make([]Jump, len(st.Jumps))
	for i, j := range st.Jumps {
		v.JumpHasPassword = append(v.JumpHasPassword, j.Password != "")
		j.Password = ""
		v.Site.Jumps[i] = j
	}
	return v
}
