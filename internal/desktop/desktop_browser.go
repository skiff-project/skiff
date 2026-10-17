//go:build !desktop

package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Native reports whether this build embeds a webview.
const Native = false

func chromiumCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		pf, pf86, local := os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")
		return []string{
			filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf, `BraveSoftware\Brave-Browser\Application\brave.exe`),
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	}
	var out []string
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func openDefault(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}

// Raise cannot bring a browser window to the front.
func Raise() {}

// Reopen shows the UI again, its browser tab having been closed (with a
// Chromium "app" window, Skiff ends when the window closes).
func Reopen(url string) { openDefault(url) }

// Run shows the UI in a Chromium-based "app" window when available (and
// returns when it closes), otherwise in the default browser (and returns
// once the UI has been gone for a while).
func Run(o Options) error {
	for _, c := range chromiumCandidates() {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		profile := filepath.Join(o.DataDir, "browser")
		cmd := exec.Command(c, "--app="+o.URL, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "--window-size=1440,900", "--class=Skiff")
		if err := cmd.Start(); err == nil {
			cmd.Wait()
			return nil
		}
	}
	if err := openDefault(o.URL); err != nil {
		return errors.Join(err, fmt.Errorf("open %s in your browser", o.URL))
	}
	if o.WaitIdle != nil {
		o.WaitIdle()
	}
	return nil
}
