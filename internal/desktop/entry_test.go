package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallEntry(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("desktop entries are for Linux and BSD")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", t.TempDir()) // no package installed
	icon := []byte("\x89PNG fake icon")
	entry, iconPath := EntryPaths()
	wrote, err := InstallEntry(icon, "/opt/skiff/skiff")
	if err != nil || !wrote {
		t.Fatalf("install: %v %v", wrote, err)
	}
	b, _ := os.ReadFile(entry)
	for _, want := range []string{"Name=Skiff", "Exec=/opt/skiff/skiff %U", "Icon=" + iconPath, "StartupWMClass=skiff", entryMark} {
		if !strings.Contains(string(b), want) {
			t.Errorf("entry lacks %q:\n%s", want, b)
		}
	}
	if got, _ := os.ReadFile(iconPath); string(got) != string(icon) {
		t.Errorf("icon %q", got)
	}
	// up to date: nothing written
	if wrote, _ := InstallEntry(icon, "/opt/skiff/skiff"); wrote {
		t.Error("written again")
	}
	// the program moved (a space in its path)
	if wrote, _ := InstallEntry(icon, "/home/alex/Mes programmes/skiff"); !wrote {
		t.Error("not updated after a move")
	}
	if b, _ := os.ReadFile(entry); !strings.Contains(string(b), `Exec="/home/alex/Mes programmes/skiff" %U`) {
		t.Errorf("quoted path:\n%s", b)
	}
	// removed
	if err := RemoveEntry(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Error("entry left")
	}
	// an entry written by the user is left alone
	os.WriteFile(entry, []byte("[Desktop Entry]\nName=Mon Skiff\nExec=/usr/bin/skiff\n"), 0o644)
	if wrote, _ := InstallEntry(icon, "/opt/skiff/skiff"); wrote {
		t.Error("the user's entry was replaced")
	}
	RemoveEntry()
	if b, _ := os.ReadFile(entry); !strings.Contains(string(b), "Mon Skiff") {
		t.Error("the user's entry was removed")
	}
}

// Installed by a package, Skiff is in the menus for every user: an entry of
// its own would hide that one, and stay behind once the package is removed.
func TestNoEntryOverAPackage(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("desktop entries are for Linux and BSD")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	sys := t.TempDir()
	t.Setenv("XDG_DATA_DIRS", t.TempDir()+string(os.PathListSeparator)+sys)
	icon := []byte("\x89PNG fake icon")
	entry, iconPath := EntryPaths()
	if _, err := InstallEntry(icon, "/home/alex/bin/skiff"); err != nil {
		t.Fatal(err)
	}
	if SystemEntry() != "" {
		t.Fatal("no package yet")
	}
	pkg := filepath.Join(sys, "applications", "skiff.desktop")
	os.MkdirAll(filepath.Dir(pkg), 0o755)
	os.WriteFile(pkg, []byte(entryText("skiff", "skiff")), 0o644)
	if SystemEntry() != pkg {
		t.Fatalf("system entry %q", SystemEntry())
	}
	if wrote, err := InstallEntry(icon, "/usr/bin/skiff"); wrote || err != nil {
		t.Fatalf("written over the package: %v %v", wrote, err)
	}
	for _, p := range []string{entry, iconPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left: it hides the package entry", p)
		}
	}
	// the data folder of the user listed among the system ones
	t.Setenv("XDG_DATA_DIRS", os.Getenv("XDG_DATA_HOME"))
	if wrote, _ := InstallEntry(icon, "/home/alex/bin/skiff"); !wrote {
		t.Error("the entry of the user taken for a package")
	}
	// the inside of the AppImage, added by its launcher
	t.Setenv("APPDIR", filepath.Dir(sys))
	t.Setenv("XDG_DATA_DIRS", sys)
	if SystemEntry() != "" {
		t.Error("the entry inside the AppImage taken for a package")
	}
}

// The entry of the packages (build/linux) is the one Skiff writes, with the
// program and the icon found by name.
func TestPackageEntryFile(t *testing.T) {
	b, err := os.ReadFile("../../build/linux/skiff.desktop")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(entryText("skiff", "skiff"), entryMark+"\n", "", 1)
	if string(b) != want {
		t.Errorf("build/linux/skiff.desktop differs from entryText:\n%s\nwant:\n%s", b, want)
	}
}
