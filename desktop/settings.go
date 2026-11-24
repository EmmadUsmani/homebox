package main

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Settings exposes host-side operations for local VMs.
type Settings struct {
	box *app.Service
}

func (s *Settings) Ports(ctx context.Context, name string) ([]vm.PortForward, error) {
	return s.box.Ports(ctx, name, app.PortOptions{})
}

func (s *Settings) UpdatePorts(ctx context.Context, name string, options app.PortOptions) ([]vm.PortForward, error) {
	return s.box.Ports(ctx, name, options)
}

func (s *Settings) PortURL(ctx context.Context, name string, port int) (string, error) {
	return s.box.Open(ctx, name, strconv.Itoa(port), true)
}

func (s *Settings) Resize(ctx context.Context, name string, size vm.Size) (vm.Size, error) {
	return s.box.Resize(ctx, name, size)
}

type SSHFingerprints struct {
	Saved    []string `json:"saved"`
	Detected []string `json:"detected"`
}

func (s *Settings) ReviewSSHKeys(ctx context.Context, name string) (SSHFingerprints, error) {
	var review SSHFingerprints
	err := s.box.Repin(ctx, name, func(saved, detected []string) (bool, error) {
		review = SSHFingerprints{Saved: saved, Detected: detected}
		return false, nil
	})
	return review, err
}

func (s *Settings) ReplaceSSHKeys(ctx context.Context, name string, review SSHFingerprints) error {
	return s.box.Repin(ctx, name, func(saved, detected []string) (bool, error) {
		if !slices.Equal(review.Saved, saved) || !slices.Equal(review.Detected, detected) {
			return false, errors.New("SSH fingerprints changed again. Review the current fingerprints before replacing the pins")
		}
		return true, nil
	})
}

func (s *Settings) SSHConfig(ctx context.Context, options app.SSHConfigOptions) (string, error) {
	return s.box.SSHConfig(ctx, options)
}

// version can be set at build time with -ldflags "-X main.version=...".
var version = "dev"

func aboutDescription() string {
	boxVersion := "unavailable"
	if path, err := bundledBox(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, path, "version").Output(); err == nil {
			boxVersion = strings.TrimSpace(string(output))
		}
	}
	return "homebox " + version + "\nbox " + boxVersion
}

func setAboutMenu(desktop *application.App) {
	menu := application.DefaultApplicationMenu()
	if about := menu.FindByRole(application.About); about != nil {
		// Use the Wails callback so the dialog can display both versions.
		about.SetRole(application.NoRole)
	}
	desktop.Menu.Set(menu)
}
