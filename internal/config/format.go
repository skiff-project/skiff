package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The format of the configuration folder, the promise of compatibility of
// Skiff 1.x: a Skiff reads the folders of the formats up to its own,
// migrates an older one after saving a copy of its files in backups/, and
// refuses a newer one (it would lose what it does not know).

// FormatVersion is the format this Skiff writes. A folder without
// format.json comes from before Skiff 1.0 (format 0).
const FormatVersion = 1

// keptBackups is the number of backups kept in backups/.
const keptBackups = 5

// ErrNewerFormat: the folder was written by a newer Skiff.
type ErrNewerFormat struct {
	Dir    string
	Format int
}

func (e ErrNewerFormat) Error() string {
	return fmt.Sprintf("the configuration in %s was written by a newer version of Skiff (format %d; this one reads up to format %d): update Skiff, or start it with another configuration folder (SKIFF_HOME)", e.Dir, e.Format, FormatVersion)
}

type formatFile struct {
	Format int `json:"format"`
}

// migrations[n] turns a folder of format n into format n+1, its files
// being backed up first. 0 → 1 changes nothing: it starts the versioning.
var migrations = map[int]func(dir string) error{
	0: func(string) error { return nil },
}

// checkFormat reads the format of dir and migrates it when it is older.
func checkFormat(dir string) error {
	p := filepath.Join(dir, "format.json")
	cur := 0
	if b, err := os.ReadFile(p); err == nil {
		var f formatFile
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("format.json: %w", err)
		}
		cur = f.Format
	} else if !hasConfiguration(dir) {
		cur = FormatVersion // a new folder
	}
	if cur > FormatVersion {
		return ErrNewerFormat{Dir: dir, Format: cur}
	}
	if cur < FormatVersion {
		if err := backup(dir, cur); err != nil {
			return fmt.Errorf("backup before migrating the configuration: %w", err)
		}
		for v := cur; v < FormatVersion; v++ {
			if m := migrations[v]; m != nil {
				if err := m(dir); err != nil {
					return fmt.Errorf("migrating the configuration from format %d: %w", v, err)
				}
			}
		}
	} else if _, err := os.Stat(p); err == nil {
		return nil
	}
	b, _ := json.Marshal(formatFile{Format: FormatVersion})
	return writeFileAtomic(p, append(b, '\n'), 0o600)
}

// hasConfiguration: dir holds files of a configuration.
func hasConfiguration(dir string) bool {
	for _, n := range []string{"sites.json", "settings.json", "keychain.json", "tasks.json", "known_hosts", "master.json"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// backup copies the files of dir (not its folders: logs, history, webview
// data) into backups/<date>-format-<n>, and keeps the last ones.
func backup(dir string, format int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	root := filepath.Join(dir, "backups")
	dst := filepath.Join(root, fmt.Sprintf("%s-format-%d", time.Now().Format("20060102-150405"), format))
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if err := copyFile(filepath.Join(dir, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	// the oldest go
	olds, _ := os.ReadDir(root)
	var names []string
	for _, o := range olds {
		if o.IsDir() {
			names = append(names, o.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keptBackups {
		os.RemoveAll(filepath.Join(root, names[0]))
		names = names[1:]
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
