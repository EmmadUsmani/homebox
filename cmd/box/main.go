// Command box creates and manages homebox development VMs.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	guestfs "github.com/EmmadUsmani/homebox/guest"
	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/cli"
	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm/lima"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	service, err := newService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "box:", err)
		os.Exit(1)
	}
	os.Exit(cli.Execute(service, version))
}

// newService wires the concrete pieces: the Lima driver in box's own Lima
// home, the embedded guest profiles, the on-disk store and the system ssh.
func newService() (*app.Service, error) {
	root, err := state.DefaultRoot()
	if err != nil {
		return nil, err
	}
	profiles, err := guest.NewCatalog(guestfs.FS)
	if err != nil {
		return nil, fmt.Errorf("load guest profiles: %w", err)
	}
	runner := execx.OS{}
	ssh := sshx.Client{Runner: runner}
	return &app.Service{
		Driver:        lima.New(lima.Options{Home: filepath.Join(root, "lima"), Runner: runner}),
		Profiles:      profiles,
		Store:         &state.Store{Root: root},
		GuestSSH:      ssh,
		Isolation:     app.NoIsolation{},
		LocalHostName: ssh.LocalHostName,
		Progress:      os.Stderr,
	}, nil
}
