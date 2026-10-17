package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableInsideAnAppImage(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	t.Setenv("APPIMAGE", "/home/alex/Applications/Skiff-x86_64.AppImage")
	t.Setenv("APPDIR", filepath.Dir(exe))
	if got, _ := Executable(); got != "/home/alex/Applications/Skiff-x86_64.AppImage" {
		t.Errorf("inside the AppImage: %q", got)
	}
	// another program started from the AppImage inherits its variables
	t.Setenv("APPDIR", t.TempDir())
	if got, _ := Executable(); got != exe {
		t.Errorf("outside the AppImage: %q, want %q", got, exe)
	}
}
