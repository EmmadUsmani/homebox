// Command box creates and manages homebox development VMs.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	guestfs "github.com/EmmadUsmani/homebox/guest"
	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/cli"
	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/isolation"
	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm/lima"
	"github.com/EmmadUsmani/homebox/internal/vmnet"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// The per-VM network daemon is box re-executing itself; it needs none of
	// the service wiring below.
	if len(os.Args) > 1 && os.Args[1] == isolation.DaemonCommand {
		os.Exit(runDaemon(os.Args[2:]))
	}
	service, err := newService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "box:", err)
		os.Exit(1)
	}
	os.Exit(cli.Execute(service, version))
}

func runDaemon(args []string) int {
	config, err := vmnet.ParseArgs(args)
	if err == nil {
		err = vmnet.Run(context.Background(), config)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "box "+isolation.DaemonCommand+":", err)
		return 1
	}
	return 0
}

// newService wires the concrete pieces: the Lima driver in box's own Lima
// home, the embedded guest profiles, the on-disk store, the system ssh and
// the host isolation.
func newService() (*app.Service, error) {
	root, err := state.DefaultRoot()
	if err != nil {
		return nil, err
	}
	profiles, err := guest.NewCatalog(guestfs.FS)
	if err != nil {
		return nil, fmt.Errorf("load guest profiles: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	runner := execx.OS{}
	ssh := sshx.Client{Runner: runner}
	store := &state.Store{Root: root}
	return &app.Service{
		Driver:        lima.New(lima.Options{Home: filepath.Join(root, "lima"), Runner: runner}),
		Profiles:      profiles,
		Store:         store,
		GuestSSH:      ssh,
		Isolation:     &isolation.Host{Store: store, Executable: exe},
		LocalHostName: ssh.LocalHostName,
		Progress:      os.Stderr,
	}, nil
}
