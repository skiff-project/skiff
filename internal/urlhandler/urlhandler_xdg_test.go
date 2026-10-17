//go:build !windows && !darwin

package urlhandler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func scheme(st Status, s string) Scheme {
	for _, x := range st.Schemes {
		if x.Scheme == s {
			return x
		}
	}
	return Scheme{}
}

func TestLinksOnLinux(t *testing.T) {
	root := t.TempDir()
	cfg, sys, data, sysData := filepath.Join(root, "config"), filepath.Join(root, "xdg"), filepath.Join(root, "data"), filepath.Join(root, "usr-share")
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_CONFIG_DIRS", sys)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", sysData)
	t.Setenv("XDG_CURRENT_DESKTOP", "ubuntu:GNOME")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "fr_FR.UTF-8")

	// the file manager of the system opens sftp:// (default of the desktop)
	write(t, filepath.Join(sysData, "applications", "org.gnome.Nautilus.desktop"), "[Desktop Entry]\nName=Files\nName[fr]=Fichiers\nExec=nautilus %U\n")
	write(t, filepath.Join(sysData, "applications", "gnome-mimeapps.list"), "[Default Applications]\nx-scheme-handler/sftp=org.gnome.Nautilus.desktop\n")
	// the user's own choices for other types, and an entry of a missing application
	write(t, filepath.Join(cfg, "mimeapps.list"), "[Default Applications]\ntext/html=firefox.desktop\nx-scheme-handler/ftp=gone.desktop;\n\n[Removed Associations]\nimage/png=gimp.desktop;\n")
	// the desktop's file of the user names another application for scp
	write(t, filepath.Join(sysData, "applications", "other.desktop"), "[Desktop Entry]\nName=Other\n")
	write(t, filepath.Join(cfg, "gnome-mimeapps.list"), "[Default Applications]\nx-scheme-handler/scp=other.desktop\n")

	st := Get()
	if !st.Supported || st.All() {
		t.Fatalf("status before: %+v", st)
	}
	if x := scheme(st, "sftp"); x.Skiff || x.Handler != "Fichiers" {
		t.Errorf("sftp before: %+v", x)
	}
	if x := scheme(st, "ftp"); x.Skiff || x.Handler != "" {
		t.Errorf("ftp before (missing application): %+v", x)
	}
	if x := scheme(st, "scp"); x.Handler != "Other" {
		t.Errorf("scp before: %+v", x)
	}

	if err := Register("/opt/skiff/skiff", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if st := Get(); !st.All() {
		t.Fatalf("status after: %+v", st)
	}
	main := read(t, filepath.Join(cfg, "mimeapps.list"))
	for _, want := range []string{"text/html=firefox.desktop", "x-scheme-handler/sftp=skiff.desktop", "x-scheme-handler/davs=skiff.desktop", "[Removed Associations]\nimage/png=gimp.desktop;", "[Added Associations]\nx-scheme-handler/sftp=skiff.desktop;"} {
		if !strings.Contains(main, want) {
			t.Errorf("mimeapps.list lacks %q:\n%s", want, main)
		}
	}
	if strings.Contains(main, "gone.desktop") {
		t.Errorf("the default for ftp was not replaced:\n%s", main)
	}
	if g := read(t, filepath.Join(cfg, "gnome-mimeapps.list")); !strings.Contains(g, "x-scheme-handler/scp=skiff.desktop") || strings.Contains(g, "sftp") {
		t.Errorf("gnome-mimeapps.list: only its own key changes:\n%s", g)
	}
	// the menu entry that lists the schemes, started with --link
	entry := read(t, filepath.Join(data, "applications", "skiff.desktop"))
	if !strings.Contains(entry, "Exec=/opt/skiff/skiff --link %U") || !strings.Contains(entry, "MimeType=x-scheme-handler/sftp;") {
		t.Errorf("menu entry:\n%s", entry)
	}

	if err := Remove("/opt/skiff/skiff"); err != nil {
		t.Fatal(err)
	}
	st = Get()
	if x := scheme(st, "sftp"); x.Skiff || x.Handler != "Fichiers" {
		t.Errorf("sftp after removal: %+v", x)
	}
	main = read(t, filepath.Join(cfg, "mimeapps.list"))
	if strings.Contains(main, "skiff.desktop") || !strings.Contains(main, "text/html=firefox.desktop") || !strings.Contains(main, "image/png=gimp.desktop;") {
		t.Errorf("mimeapps.list after removal:\n%s", main)
	}
	if g := read(t, filepath.Join(cfg, "gnome-mimeapps.list")); strings.Contains(g, "skiff") {
		t.Errorf("gnome-mimeapps.list after removal:\n%s", g)
	}
}

// The package's menu entry lists the schemes Skiff opens.
func TestPackageEntryListsTheSchemes(t *testing.T) {
	b, err := os.ReadFile("../../build/linux/skiff.desktop")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, s := range Schemes {
		want = append(want, "x-scheme-handler/"+s)
	}
	if !strings.Contains(string(b), "\nMimeType="+strings.Join(want, ";")+";\n") {
		t.Errorf("MimeType of build/linux/skiff.desktop differs from %v:\n%s", Schemes, b)
	}
}

func TestINI(t *testing.T) {
	f := &iniFile{lines: []string{"# comment", "[A]", "k = v", "", "[B]", "x=1"}}
	if f.get("A", "k") != "v" || f.get("B", "x") != "1" || f.has("A", "x") {
		t.Fatal("get")
	}
	f.set("A", "n", "2")
	f.set("C", "z", "3")
	f.del("B", "x")
	want := "# comment\n[A]\nk = v\nn=2\n\n[B]\n\n[C]\nz=3\n"
	if got := string(f.bytes()); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
