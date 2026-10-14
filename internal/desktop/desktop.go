// Package desktop shows the Skiff UI: a native window (Wails, build tag
// "desktop") or, in builds without a webview, a Chromium/Edge "app" window
// or the default browser.
package desktop

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
	Icon []byte
	DataDir string
}
