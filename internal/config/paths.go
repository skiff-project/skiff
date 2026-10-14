// Package config persists preferences, saved sites (with encrypted
// secrets), trusted certificates and other application state.
package config

import (
	"os"
	"path/filepath"
)

// Dir resolves the configuration directory:
//   - $SKIFF_HOME when set,
//   - "skiff-data" next to the executable when it exists or when a file
//     named "skiff.portable" sits next to it (portable mode, like WinSCP.ini),
//   - otherwise <user config dir>/Skiff.
func Dir() (string, bool) {
	if d := os.Getenv("SKIFF_HOME"); d != "" {
		return d, false
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		base := filepath.Dir(exe)
		data := filepath.Join(base, "skiff-data")
		if st, err := os.Stat(data); err == nil && st.IsDir() {
			return data, true
		}
		if _, err := os.Stat(filepath.Join(base, "skiff.portable")); err == nil {
			return data, true
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "Skiff"), false
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".skiff"), false
}

// writeFileAtomic writes data to path through a temporary file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	os.Chmod(name, perm)
	return os.Rename(name, path)
}
