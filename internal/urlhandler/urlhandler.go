// Package urlhandler makes Skiff the application that opens the sftp://,
// scp://, ftp://… links of the system (a browser, a mail, a document): the
// system then starts "skiff --link <url>", and the window asks before
// connecting. Linux and BSD use mimeapps.list (freedesktop), Windows the
// registry of the user, macOS LaunchServices.
package urlhandler

import "errors"

// Schemes are the links Skiff opens (WinSCP opens the same ones).
var Schemes = []string{"sftp", "scp", "ftp", "ftps", "ftpes", "dav", "davs"}

// Scheme tells which application opens the links of a scheme.
type Scheme struct {
	Scheme string `json:"scheme"`
	Skiff  bool   `json:"skiff"`
	// Handler is the application that opens them ("" none, or unknown)
	Handler string `json:"handler,omitempty"`
}

// Status of the links on this system.
type Status struct {
	// Supported: Skiff can make itself the application of the links here
	Supported bool `json:"supported"`
	// CanRemove: and give them back (macOS keeps a handler until another
	// application takes the links)
	CanRemove bool     `json:"canRemove"`
	Schemes   []Scheme `json:"schemes"`
	// Settings: the default applications of the system can be opened
	// (another application may keep the links there, on Windows)
	Settings bool `json:"settings"`
	// Packaged: Skiff runs from its Microsoft Store package (MSIX), whose
	// manifest declares the links: they are chosen in the settings only
	Packaged bool `json:"packaged,omitempty"`
}

// ErrUnsupported: no way to register the links on this system or build.
var ErrUnsupported = errors.New("this system or build of Skiff cannot open links")

// ErrPackaged: the Microsoft Store version declares its links in its
// package; only the settings of Windows choose the application.
var ErrPackaged = errors.New("the Microsoft Store version of Skiff declares these links in its package: choose Skiff for them in the default apps of Windows")

// All reports whether Skiff opens the links of every scheme.
func (s Status) All() bool {
	for _, x := range s.Schemes {
		if !x.Skiff {
			return false
		}
	}
	return len(s.Schemes) > 0
}

// Get tells which application opens each scheme.
func Get() Status { return get() }

// Register makes Skiff (exe, with its icon for the menu entry it may need)
// open the links of every scheme.
func Register(exe string, icon []byte) error { return register(exe, icon) }

// Remove gives the links back: Skiff no longer opens them.
func Remove(exe string) error { return remove(exe) }

// OpenSettings opens the default applications of the system.
func OpenSettings() error { return openSettings() }
