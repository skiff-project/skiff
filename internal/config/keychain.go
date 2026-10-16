package config

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// KeychainKey is a private key kept by Skiff. The private part is encrypted
// like the passwords (by the master password when there is one) and the
// keys are offered to every SSH server, like those of an SSH agent.
type KeychainKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"publicKey"` // authorized_keys line
	Private     string    `json:"private,omitempty"`
	Added       time.Time `json:"added"`
	Updated     time.Time `json:"updated"`
}

var ErrKeyExists = errors.New("this key is already in the keychain")

func (s *Store) loadKeychain() {
	if data, err := os.ReadFile(s.path("keychain.json")); err == nil {
		json.Unmarshal(data, &s.keys)
	}
}

func (s *Store) saveKeychainLocked() error {
	data, err := json.MarshalIndent(s.keys, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path("keychain.json"), data, 0o600)
}

// Keys lists the keychain without the private keys.
func (s *Store) Keys() []KeychainKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]KeychainKey, len(s.keys))
	for i, k := range s.keys {
		k.Private = ""
		out[i] = k
	}
	return out
}

// AddKey stores a key; private is its unencrypted OpenSSH PEM.
func (s *Store) AddKey(k KeychainKey, private string) (KeychainKey, error) {
	enc, err := s.Secrets.Encrypt(private)
	if err != nil {
		return KeychainKey{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.keys {
		if o.Fingerprint == k.Fingerprint {
			return KeychainKey{}, ErrKeyExists
		}
	}
	k.ID, k.Private, k.Added = NewID(), enc, time.Now()
	k.Updated = k.Added
	s.keys = append(s.keys, k)
	if err := s.saveKeychainLocked(); err != nil {
		return KeychainKey{}, err
	}
	k.Private = ""
	return k, nil
}

func (s *Store) RenameKey(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.keys {
		if s.keys[i].ID == id {
			s.keys[i].Name, s.keys[i].Updated = name, time.Now()
			return s.saveKeychainLocked()
		}
	}
	return errors.New("key not found")
}

func (s *Store) RemoveKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.keys {
		if s.keys[i].ID == id {
			s.buryLocked("key:" + s.keys[i].Fingerprint)
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			return s.saveKeychainLocked()
		}
	}
	return errors.New("key not found")
}

// KeyPrivates returns the unencrypted private keys (ErrLocked while the
// master password is locked).
func (s *Store) KeyPrivates() ([]string, error) {
	s.mu.RLock()
	keys := append([]KeychainKey(nil), s.keys...)
	s.mu.RUnlock()
	var out []string
	for _, k := range keys {
		p, err := s.Secrets.Decrypt(k.Private)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
