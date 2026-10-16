package config

import (
	"encoding/json"
	"os"
	"time"
)

// What the synchronization between devices needs from the store: the
// deletions to propagate ("tombstones"), and the sites and keys with their
// secrets in clear, to be re-encrypted in the shared file.

const tombstoneLife = 180 * 24 * time.Hour

func (s *Store) loadTombstones() {
	s.tombs = map[string]time.Time{}
	if b, err := os.ReadFile(s.path("sync-tombstones.json")); err == nil {
		json.Unmarshal(b, &s.tombs)
	}
}

func (s *Store) saveTombstonesLocked() error {
	for k, t := range s.tombs {
		if time.Since(t) > tombstoneLife {
			delete(s.tombs, k)
		}
	}
	b, _ := json.MarshalIndent(s.tombs, "", "  ")
	return writeFileAtomic(s.path("sync-tombstones.json"), b, 0o600)
}

// buryLocked records the deletion of an item ("site:<id>", "key:<fingerprint>",
// "folder:<name>").
func (s *Store) buryLocked(keys ...string) {
	now := time.Now()
	for _, k := range keys {
		s.tombs[k] = now
	}
	s.saveTombstonesLocked()
}

// Tombstones returns the deletions recorded on this device.
func (s *Store) Tombstones() map[string]time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]time.Time, len(s.tombs))
	for k, v := range s.tombs {
		out[k] = v
	}
	return out
}

// MergeTombstones adds deletions learnt from other devices.
func (s *Store) MergeTombstones(in map[string]time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range in {
		if t.After(s.tombs[k]) {
			s.tombs[k] = t
		}
	}
	return s.saveTombstonesLocked()
}

// SitesInClear returns every site with its secrets decrypted (ErrLocked
// while the master password is locked).
func (s *Store) SitesInClear() ([]Site, []string, error) {
	sites, folders := s.Sites(), s.Folders()
	out := make([]Site, 0, len(sites))
	for _, st := range sites {
		r, err := s.Resolve(st)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, r)
	}
	return out, folders, nil
}

// ReplaceSites replaces every site and folder; the secrets of sites are
// given in clear and encrypted here. The local use counters are kept.
func (s *Store) ReplaceSites(sites []Site, folders []string) error {
	enc := func(v *string) error {
		if *v == "" {
			return nil
		}
		e, err := s.Secrets.Encrypt(*v)
		*v = e
		return err
	}
	out := make([]Site, 0, len(sites))
	for _, st := range sites {
		st.Jumps = append([]Jump(nil), st.Jumps...)
		for _, v := range []*string{&st.Password, &st.Passphrase, &st.Proxy.Password, &st.S3SessionToken} {
			if err := enc(v); err != nil {
				return err
			}
		}
		for i := range st.Jumps {
			if err := enc(&st.Jumps[i].Password); err != nil {
				return err
			}
		}
		st.ApplyDefaults()
		out = append(out, st)
	}
	s.mu.Lock()
	local := map[string]Site{}
	for _, st := range s.sites.Sites {
		local[st.ID] = st
	}
	for i := range out {
		if l, ok := local[out[i].ID]; ok {
			out[i].LastUsed, out[i].UseCount = l.LastUsed, l.UseCount
		}
	}
	s.sites.Sites, s.sites.Folders = out, append([]string(nil), folders...)
	err := s.saveSitesLocked()
	s.mu.Unlock()
	return err
}

// KeysInClear returns the keychain with the private keys decrypted.
func (s *Store) KeysInClear() ([]KeychainKey, error) {
	s.mu.RLock()
	keys := append([]KeychainKey(nil), s.keys...)
	s.mu.RUnlock()
	for i := range keys {
		p, err := s.Secrets.Decrypt(keys[i].Private)
		if err != nil {
			return nil, err
		}
		keys[i].Private = p
	}
	return keys, nil
}

// ReplaceKeys replaces the keychain; private keys are given in clear.
func (s *Store) ReplaceKeys(keys []KeychainKey) error {
	out := make([]KeychainKey, len(keys))
	for i, k := range keys {
		e, err := s.Secrets.Encrypt(k.Private)
		if err != nil {
			return err
		}
		k.Private = e
		out[i] = k
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = out
	return s.saveKeychainLocked()
}
