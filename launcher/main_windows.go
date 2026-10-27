//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

//go:embed ui.html
var uiHTML string

func main() {
	runtime.LockOSThread()

	local, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		messageBox("Porotos 2", "No se encontró la carpeta AppData.")
		return
	}
	paths := NewPaths(filepath.Join(local, "Porotos2Launcher"))
	_ = os.MkdirAll(paths.Data, 0o755)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(paths.Data, "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  "Porotos 2 Launcher",
			Width:  780,
			Height: 500,
			Center: true,
		},
	})
	if w == nil {
		messageBox("Porotos 2",
			"Este launcher necesita Microsoft Edge WebView2.\n\nDescargalo gratis desde:\nhttps://developer.microsoft.com/microsoft-edge/webview2/")
		return
	}
	defer w.Destroy()
	w.SetSize(780, 500, webview2.HintFixed)

	emit := func(event string, payload interface{}) {
		b, _ := json.Marshal(payload)
		ev, _ := json.Marshal(event)
		w.Dispatch(func() {
			w.Eval("window.onEvent && window.onEvent(" + string(ev) + "," + string(b) + ")")
		})
	}

	app := NewApp(paths, winPlatform{}, emit)
	_ = w.Bind("refresh", func() { app.Refresh() })
	_ = w.Bind("refreshLocal", func() { app.RefreshLocal() })
	_ = w.Bind("install", func(force bool) { app.Install(force) })
	_ = w.Bind("play", func() { app.Play() })
	_ = w.Bind("openFolder", func() { app.OpenGameFolder() })
	_ = w.Bind("openRelease", func() { app.OpenRelease() })
	_ = w.Bind("closeLauncher", func() {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			w.Dispatch(func() { w.Terminate() })
		}()
	})

	w.SetHtml(uiHTML)
	w.Run()
}
