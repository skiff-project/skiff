//go:build desktop

package desktop

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"os"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Native reports whether this build embeds a webview.
const Native = true

// Run opens the native window and blocks until it is closed.
func Run(o Options) error {
	if goruntime.GOOS == "linux" {
		// GTK formats CSS colors with printf("%f"): under locales with a
		// decimal comma (fr_FR…) the window background CSS becomes invalid.
		os.Setenv("LC_NUMERIC", "C")
	}
	boot := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>%s</title>
<style>html,body{margin:0;height:100%%;background:#14161c}</style></head>
<body><script>location.replace(%q)</script></body></html>`, html.EscapeString(o.Title), o.URL)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(boot))
	})
	return wails.Run(&options.App{
		Title:                    o.Title,
		Width:                    1440,
		Height:                   900,
		MinWidth:                 860,
		MinHeight:                560,
		BackgroundColour:         &options.RGBA{R: 20, G: 22, B: 28, A: 255},
		AssetServer:              &assetserver.Options{Handler: handler},
		EnableDefaultContextMenu: false,
		OnStartup: func(ctx context.Context) {
			setClipboard(func() (string, error) { return runtime.ClipboardGetText(ctx) }, func(s string) error { return runtime.ClipboardSetText(ctx, s) })
		},
		OnBeforeClose: func(ctx context.Context) bool {
			if o.Busy == nil {
				return false
			}
			n := o.Busy()
			if n == 0 {
				return false
			}
			res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
				Type:          runtime.QuestionDialog,
				Title:         "Transfers in progress",
				Message:       fmt.Sprintf("%d transfer(s) are still running. Quit Skiff and cancel them?", n),
				Buttons:       []string{"Quit", "Cancel"},
				DefaultButton: "Cancel",
				CancelButton:  "Cancel",
			})
			if err != nil {
				return false
			}
			return res != "Quit" && res != "Yes"
		},
		Linux: &linux.Options{
			ProgramName:      "skiff",
			Icon:             o.Icon,
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
		Windows: &windows.Options{
			Theme:               windows.SystemDefault,
			WebviewUserDataPath: o.DataDir,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarDefault(),
			About:    &mac.AboutInfo{Title: "Skiff", Message: "Version " + o.Version + "\nModern cross-platform file transfer client", Icon: o.Icon},
		},
	})
}
