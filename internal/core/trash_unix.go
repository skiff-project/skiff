//go:build !windows

package core

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// moveToTrash moves a local file to the desktop trash (freedesktop.org
// specification on Linux/BSD, ~/.Trash on macOS).
func moveToTrash(p string) error {
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		dst := uniqueTrashName(filepath.Join(home, ".Trash"), filepath.Base(abs))
		return os.Rename(abs, dst)
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	trash := filepath.Join(data, "Trash")
	files, info := filepath.Join(trash, "files"), filepath.Join(trash, "info")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(info, 0o700); err != nil {
		return err
	}
	dst := uniqueTrashName(files, filepath.Base(abs))
	name := filepath.Base(dst)
	u := &url.URL{Path: abs}
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", strings.TrimPrefix(u.EscapedPath(), ""), time.Now().Format("2006-01-02T15:04:05"))
	infoPath := filepath.Join(info, name+".trashinfo")
	if err := os.WriteFile(infoPath, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(abs, dst); err != nil {
		os.Remove(infoPath)
		// different file system: delete permanently is not what the user
		// asked for, report instead
		return fmt.Errorf("cannot move %s to the trash: %w", p, err)
	}
	return nil
}

func uniqueTrashName(dir, base string) string {
	cand := filepath.Join(dir, base)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; ; i++ {
		if _, err := os.Lstat(cand); os.IsNotExist(err) {
			return cand
		}
		cand = filepath.Join(dir, fmt.Sprintf("%s.%d%s", stem, i, ext))
	}
}
