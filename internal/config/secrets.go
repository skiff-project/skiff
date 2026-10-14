package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Secrets encrypts the passwords stored in sites.json.
//
// Without a master password a random key kept in "secret.key" (mode 0600) is
// used, which protects the file when it is shared or synced but not against
// someone with access to the account. With a master password the key is
// derived with Argon2id and never written to disk.
type Secrets struct {
	mu       sync.RWMutex
	keyPath  string
	metaPath string
	local    []byte // key from secret.key
	master   []byte // derived master key (nil when locked)
	meta     *masterMeta
}

type masterMeta struct {
	Salt     string `json:"salt"`
	Time     uint32 `json:"time"`
	Memory   uint32 `json:"memory"`
	Threads  uint8  `json:"threads"`
	Verifier string `json:"verifier"`
}

var (
	ErrLocked        = errors.New("secrets are locked by the master password")
	ErrWrongPassword = errors.New("wrong master password")
)

const (
	prefixLocal  = "enc1:"
	prefixMaster = "mp1:"
	verifierText = "skiff-master-password-v1"
)

func newSecrets(dir string) (*Secrets, error) {
	s := &Secrets{keyPath: dir + string(os.PathSeparator) + "secret.key", metaPath: dir + string(os.PathSeparator) + "master.json"}
	if data, err := os.ReadFile(s.keyPath); err == nil {
		k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(k) == chacha20poly1305.KeySize {
			s.local = k
		}
	}
	if s.local == nil {
		k := make([]byte, chacha20poly1305.KeySize)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(s.keyPath, []byte(base64.StdEncoding.EncodeToString(k)), 0o600); err != nil {
			return nil, err
		}
		s.local = k
	}
	if data, err := os.ReadFile(s.metaPath); err == nil {
		var m masterMeta
		if json.Unmarshal(data, &m) == nil && m.Salt != "" {
			s.meta = &m
		}
	}
	return s, nil
}

// HasMaster reports whether a master password is configured.
func (s *Secrets) HasMaster() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta != nil
}

// Locked reports whether a master password is set but not entered yet.
func (s *Secrets) Locked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta != nil && s.master == nil
}

func derive(password string, m *masterMeta) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(m.Salt)
	if err != nil {
		return nil, err
	}
	return argon2.IDKey([]byte(password), salt, m.Time, m.Memory, m.Threads, chacha20poly1305.KeySize), nil
}

func seal(key []byte, plain string) (string, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func open(key []byte, enc string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	if len(data) < aead.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Unlock verifies the master password and keeps the derived key in memory.
func (s *Secrets) Unlock(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meta == nil {
		return nil
	}
	k, err := derive(password, s.meta)
	if err != nil {
		return err
	}
	v, err := open(k, s.meta.Verifier)
	if err != nil || subtle.ConstantTimeCompare([]byte(v), []byte(verifierText)) != 1 {
		return ErrWrongPassword
	}
	s.master = k
	return nil
}

// Lock forgets the derived master key.
func (s *Secrets) Lock() {
	s.mu.Lock()
	s.master = nil
	s.mu.Unlock()
}

// Encrypt encodes a secret for storage. Empty stays empty.
func (s *Secrets) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.meta != nil {
		if s.master == nil {
			return "", ErrLocked
		}
		c, err := seal(s.master, plain)
		return prefixMaster + c, err
	}
	c, err := seal(s.local, plain)
	return prefixLocal + c, err
}

// Decrypt decodes a stored secret. Unprefixed values are returned as is
// (hand-edited files).
func (s *Secrets) Decrypt(stored string) (string, error) {
	switch {
	case stored == "":
		return "", nil
	case strings.HasPrefix(stored, prefixMaster):
		s.mu.RLock()
		k := s.master
		s.mu.RUnlock()
		if k == nil {
			return "", ErrLocked
		}
		return open(k, stored[len(prefixMaster):])
	case strings.HasPrefix(stored, prefixLocal):
		return open(s.local, stored[len(prefixLocal):])
	}
	return stored, nil
}

// SetMaster changes, sets (old may be empty) or removes (new empty) the
// master password, re-encrypting every secret through reencrypt.
func (s *Secrets) SetMaster(old, newPassword string, reencrypt func(decrypt func(string) (string, error), encrypt func(string) (string, error)) error) error {
	if s.HasMaster() {
		if err := s.Unlock(old); err != nil {
			return err
		}
	}
	s.mu.Lock()
	oldMaster, oldMeta := s.master, s.meta
	var newKey []byte
	var newMeta *masterMeta
	if newPassword != "" {
		salt := make([]byte, 16)
		rand.Read(salt)
		newMeta = &masterMeta{Salt: base64.StdEncoding.EncodeToString(salt), Time: 3, Memory: 64 * 1024, Threads: 2}
		k, err := derive(newPassword, newMeta)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		v, err := seal(k, verifierText)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		newMeta.Verifier = v
		newKey = k
	}
	s.mu.Unlock()

	decrypt := func(stored string) (string, error) {
		switch {
		case strings.HasPrefix(stored, prefixMaster):
			if oldMaster == nil {
				return "", ErrLocked
			}
			return open(oldMaster, stored[len(prefixMaster):])
		case strings.HasPrefix(stored, prefixLocal):
			return open(s.local, stored[len(prefixLocal):])
		}
		return stored, nil
	}
	encrypt := func(plain string) (string, error) {
		if plain == "" {
			return "", nil
		}
		if newKey != nil {
			c, err := seal(newKey, plain)
			return prefixMaster + c, err
		}
		c, err := seal(s.local, plain)
		return prefixLocal + c, err
	}
	if err := reencrypt(decrypt, encrypt); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.master, s.meta = newKey, newMeta
	if newMeta == nil {
		os.Remove(s.metaPath)
		return nil
	}
	data, _ := json.MarshalIndent(newMeta, "", "  ")
	if err := writeFileAtomic(s.metaPath, data, 0o600); err != nil {
		s.master, s.meta = oldMaster, oldMeta
		return err
	}
	return nil
}
