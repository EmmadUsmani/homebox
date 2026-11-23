// Command homebox is the desktop status spike.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/wiring"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

type Status struct {
	service *app.Service
}

func (s *Status) ListStatus() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statuses, err := s.service.ListStatus(ctx)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(statuses, "", "  ")
	return string(data), err
}

func main() {
	service, err := wiring.NewService(nil)
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
		Services: []application.Service{application.NewService(&Status{service: service})},
	})
	desktop.Window.NewWithOptions(application.WebviewWindowOptions{Title: "homebox", URL: "/"})
	if err := desktop.Run(); err != nil {
		log.Fatal(err)
	}
}
