//go:build !windows && !darwin

package urlhandler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"skiff/internal/desktop"
)

// The freedesktop way: the default application of x-scheme-handler/sftp
// is in a mimeapps.list file (the user's first, then the desktop's and the
// system's), and Skiff's desktop entry lists the schemes it opens.

const desktopID = "skiff.desktop"

const (
	defaults = "Default Applications"
	added    = "Added Associations"
)

func mimeType(scheme string) string { return "x-scheme-handler/" + scheme }

func home(sub ...string) string {
	h, _ := os.UserHomeDir()
	return filepath.Join(append([]string{h}, sub...)...)
}

func envDir(k string, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDirs(k string, def string) []string {
	v := os.Getenv(k)
	if v == "" {
		v = def
	}
	var out []string
	for _, d := range filepath.SplitList(v) {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

func configHome() string   { return envDir("XDG_CONFIG_HOME", home(".config")) }
func configDirs() []string { return envDirs("XDG_CONFIG_DIRS", "/etc/xdg") }
func dataHome() string     { return envDir("XDG_DATA_HOME", home(".local", "share")) }
func dataDirs() []string   { return envDirs("XDG_DATA_DIRS", "/usr/local/share:/usr/share") }

// desktops are the names of the current desktop (gnome, kde…), for the
// "<desktop>-mimeapps.list" files.
func desktops() []string {
	var out []string
	for _, d := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// listsIn are the mimeapps.list files of a folder, the desktop's first.
func listsIn(dir string) []string {
	var out []string
	for _, d := range desktops() {
		out = append(out, filepath.Join(dir, d+"-mimeapps.list"))
	}
	return append(out, filepath.Join(dir, "mimeapps.list"))
}

// lists are all the mimeapps.list files, the most important first.
func lists() []string {
	out := listsIn(configHome())
	for _, d := range configDirs() {
		out = append(out, listsIn(d)...)
	}
	out = append(out, listsIn(filepath.Join(dataHome(), "applications"))...)
	for _, d := range dataDirs() {
		out = append(out, listsIn(filepath.Join(d, "applications"))...)
	}
	return out
}

// desktopFile finds the desktop entry of an application.
func desktopFile(id string) string {
	for _, d := range append([]string{dataHome()}, dataDirs()...) {
		p := filepath.Join(d, "applications", id)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// defaultFor is the application that opens a MIME type, "" none.
func defaultFor(mt string) string {
	for _, f := range lists() {
		for _, id := range splitList(readINI(f).get(defaults, mt)) {
			if desktopFile(id) != "" {
				return id
			}
		}
	}
	return ""
}

// appName is the name an application shows (Name= of its entry, in the
// language of the user when the entry has it).
func appName(id string) string {
	ini := readINI(desktopFile(id))
	var keys []string
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			lang, _, _ := strings.Cut(v, ".")
			keys = append(keys, "Name["+lang+"]")
			if l, _, ok := strings.Cut(lang, "_"); ok {
				keys = append(keys, "Name["+l+"]")
			}
			break
		}
	}
	for _, k := range append(keys, "Name") {
		if n := ini.get("Desktop Entry", k); n != "" {
			return n
		}
	}
	return strings.TrimSuffix(id, ".desktop")
}

func get() Status {
	st := Status{Supported: true, CanRemove: true}
	for _, s := range Schemes {
		id := defaultFor(mimeType(s))
		x := Scheme{Scheme: s, Skiff: id == desktopID}
		if id != "" {
			x.Handler = appName(id)
		}
		st.Schemes = append(st.Schemes, x)
	}
	return st
}

func register(exe string, icon []byte) error {
	// the entry that lists the schemes: the package's, else the user's
	if desktop.SystemEntry() == "" {
		if exe == "" {
			return errors.New("cannot tell where this program is")
		}
		if _, err := desktop.InstallEntry(icon, exe); err != nil {
			return err
		}
	}
	return editUserLists(func(ini *iniFile, main bool) {
		for _, s := range Schemes {
			mt := mimeType(s)
			// a file of the desktop that names another application wins
			// over mimeapps.list: change it too
			if main || ini.has(defaults, mt) {
				ini.set(defaults, mt, desktopID)
			}
			if main {
				ini.set(added, mt, joinList(append([]string{desktopID}, without(splitList(ini.get(added, mt)), desktopID)...)))
			}
		}
	})
}

func remove(exe string) error {
	return editUserLists(func(ini *iniFile, main bool) {
		for _, s := range Schemes {
			mt := mimeType(s)
			for _, sec := range []string{defaults, added} {
				if !ini.has(sec, mt) {
					continue
				}
				if rest := without(splitList(ini.get(sec, mt)), desktopID); len(rest) > 0 {
					ini.set(sec, mt, joinList(rest))
				} else {
					ini.del(sec, mt)
				}
			}
		}
	})
}

// editUserLists changes the mimeapps.list of the user (main) and those of
// its desktops that exist.
func editUserLists(edit func(ini *iniFile, main bool)) error {
	files := listsIn(configHome())
	for i, f := range files {
		main := i == len(files)-1
		if _, err := os.Stat(f); err != nil && !main {
			continue
		}
		ini := readINI(f)
		before := string(ini.bytes())
		edit(ini, main)
		if string(ini.bytes()) == before {
			continue
		}
		if err := writeAtomic(f, ini.bytes()); err != nil {
			return err
		}
	}
	return nil
}

func openSettings() error { return ErrUnsupported }

func splitList(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ";") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func joinList(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return strings.Join(l, ";") + ";"
}

func without(l []string, x string) []string {
	var out []string
	for _, y := range l {
		if y != x {
			out = append(out, y)
		}
	}
	return out
}

func writeAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".skiff-tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// iniFile is a desktop-style ini file, kept line by line so that what
// Skiff does not change stays as it is.
type iniFile struct{ lines []string }

func readINI(p string) *iniFile {
	f := &iniFile{}
	if p == "" {
		return f
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return f
	}
	s := strings.TrimRight(string(b), "\n")
	if s != "" {
		f.lines = strings.Split(s, "\n")
	}
	return f
}

func (f *iniFile) bytes() []byte {
	if len(f.lines) == 0 {
		return nil
	}
	return []byte(strings.Join(f.lines, "\n") + "\n")
}

// find returns the line of the key (-1 if absent) and where the section
// ends (-1 if there is no such section).
func (f *iniFile) find(section, key string) (line, end int) {
	line, end = -1, -1
	in := false
	for i, l := range f.lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			if in {
				return line, i
			}
			in = t[1:len(t)-1] == section
			continue
		}
		if !in {
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key && line < 0 {
			line = i
		}
	}
	if in {
		end = len(f.lines)
	}
	return line, end
}

func (f *iniFile) has(section, key string) bool {
	l, _ := f.find(section, key)
	return l >= 0
}

func (f *iniFile) get(section, key string) string {
	l, _ := f.find(section, key)
	if l < 0 {
		return ""
	}
	_, v, _ := strings.Cut(f.lines[l], "=")
	return strings.TrimSpace(v)
}

func (f *iniFile) set(section, key, value string) {
	l, end := f.find(section, key)
	kv := key + "=" + value
	switch {
	case l >= 0:
		f.lines[l] = kv
	case end >= 0:
		// after the last key of the section (not after its blank lines)
		at := end
		for at > 0 && strings.TrimSpace(f.lines[at-1]) == "" {
			at--
		}
		f.lines = append(f.lines[:at], append([]string{kv}, f.lines[at:]...)...)
	default:
		if len(f.lines) > 0 && strings.TrimSpace(f.lines[len(f.lines)-1]) != "" {
			f.lines = append(f.lines, "")
		}
		f.lines = append(f.lines, "["+section+"]", kv)
	}
}

func (f *iniFile) del(section, key string) {
	if l, _ := f.find(section, key); l >= 0 {
		f.lines = append(f.lines[:l], f.lines[l+1:]...)
	}
}
