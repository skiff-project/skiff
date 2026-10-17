// Package desktop shows the Skiff UI: a native window (Wails, build tag
// "desktop") or, in builds without a webview, a Chromium/Edge "app" window
// or the default browser.
package desktop

import (
	"errors"
	"sync"
)

// Options for Run.
type Options struct {
	URL     string // loopback URL including the launch token
	Title   string
	Version string
	// Busy reports whether closing should be confirmed (transfers running).
	Busy func() int
	// Idle is called by the browser fallback; it returns once no UI has
	// been attached for a while.
	WaitIdle func()
	Icon     []byte
	DataDir  string
	// OnURL receives the session URLs the system opens with Skiff while it
	// runs (macOS sends them to the application instead of starting it
	// again with the URL).
	OnURL func(url string)
}

// The clipboard of the system, through the native window: a webview may
// refuse the page the clipboard API (WebKitGTK does). Without a native
// window, there is none.
var clip struct {
	sync.Mutex
	get func() (string, error)
	set func(string) error
}

// ErrNoClipboard: no native window to reach the clipboard of the system.
var ErrNoClipboard = errors.New("no system clipboard without the native window")

func setClipboard(get func() (string, error), set func(string) error) {
	clip.Lock()
	clip.get, clip.set = get, set
	clip.Unlock()
}

// ClipboardText reads the clipboard of the system.
func ClipboardText() (string, error) {
	clip.Lock()
	get := clip.get
	clip.Unlock()
	if get == nil {
		return "", ErrNoClipboard
	}
	return get()
}

// SetClipboardText writes the clipboard of the system.
func SetClipboardText(s string) error {
	clip.Lock()
	set := clip.set
	clip.Unlock()
	if set == nil {
		return ErrNoClipboard
	}
	return set(s)
}
