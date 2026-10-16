package desktop

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestInstallEntry(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("desktop entries are for Linux and BSD")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
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
