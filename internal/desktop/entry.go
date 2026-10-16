package desktop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// On Linux, the desktop (GNOME, KDE…) shows the icon and the name of a
// window from its desktop entry, found by the program name ("skiff"); the
// icon a window gives is ignored under Wayland. Skiff installs its own entry
// and icon in the data folder of the user, and keeps them up to date when
// the program moves. An entry written by hand (without the Skiff mark) is
// left alone.

const entryMark = "X-Skiff-Managed=true"

// dataHome is $XDG_DATA_HOME, or ~/.local/share.
func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".local", "share")
}

// EntryPaths are the desktop entry and the icon Skiff installs.
func EntryPaths() (entry, icon string) {
	d := dataHome()
	if d == "" {
		return "", ""
	}
	return filepath.Join(d, "applications", "skiff.desktop"), filepath.Join(d, "icons", "hicolor", "512x512", "apps", "skiff.png")
}

func entryText(exe, icon string) string {
	quoted := exe
	if strings.ContainsAny(exe, " \t\"'\\$`") {
		quoted = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(exe) + `"`
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Skiff",
		"GenericName=File transfer client",
		"GenericName[fr]=Client de transfert de fichiers",
		"Comment=SFTP, SCP, FTP, WebDAV and S3 files, terminal and editor",
		"Comment[fr]=Fichiers SFTP, SCP, FTP, WebDAV et S3, terminal et éditeur",
		"Exec=" + quoted + " %U",
		"Icon=" + icon,
		"Terminal=false",
		"Categories=Network;FileTransfer;",
		"Keywords=sftp;scp;ftp;webdav;s3;ssh;winscp;",
		"StartupWMClass=skiff",
		"StartupNotify=true",
		entryMark,
		"",
	}, "\n")
}

// InstallEntry writes (or updates) the desktop entry and the icon of Skiff
// for the user, on Linux and BSD. It does nothing elsewhere, and reports
// whether something was written.
func InstallEntry(icon []byte, exe string) (bool, error) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return false, nil
	}
	entry, iconPath := EntryPaths()
	if entry == "" {
		return false, errors.New("no data folder")
	}
	want := entryText(exe, iconPath)
	if cur, err := os.ReadFile(entry); err == nil {
		if !bytes.Contains(cur, []byte(entryMark)) {
			return false, nil // written by the user
		}
		if string(cur) == want {
			if st, err := os.Stat(iconPath); err == nil && st.Size() == int64(len(icon)) {
				return false, nil
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(iconPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(iconPath, icon, 0o644); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(entry, []byte(want), 0o644)
}

// RemoveEntry removes the desktop entry and the icon Skiff installed.
func RemoveEntry() error {
	entry, iconPath := EntryPaths()
	if cur, err := os.ReadFile(entry); err == nil && bytes.Contains(cur, []byte(entryMark)) {
		if err := os.Remove(entry); err != nil {
			return err
		}
	}
	if err := os.Remove(iconPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
