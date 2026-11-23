// Command homebox is the desktop VM app.
package main

import (
	"embed"
	"io/fs"
	"log"
	"os/exec"

	"github.com/EmmadUsmani/homebox/internal/wiring"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	service, err := wiring.NewDesktopService(bundledBox)
	if err != nil {
		log.Fatal(err)
	}
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	desktop := application.New(application.Options{
		Name: "homebox",
		Mac:  application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(frontend),
		},
	})
	desktop.RegisterService(application.NewService(&Local{
		box: service, load: service.Store.Load, findTool: exec.LookPath, emit: desktop.Event.Emit,
	}))
	desktop.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "homebox", URL: "/", Width: 1280, Height: 800, MinWidth: 850, MinHeight: 540,
		BackgroundColour: application.NewRGB(15, 17, 21),
		Mac:              application.MacWindow{TitleBar: application.MacTitleBarHiddenInset},
	})
	if err := desktop.Run(); err != nil {
		log.Fatal(err)
	}
}
