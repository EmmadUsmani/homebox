package wiring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	guestfs "github.com/EmmadUsmani/homebox/guest"
	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/hostinfo"
	"github.com/EmmadUsmani/homebox/internal/isolation"
	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm/lima"
)

// NewService wires the local service. Read-only callers can omit the daemon executable.
func NewService(executable func() (string, error)) (*app.Service, error) {
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
	store := &state.Store{Root: root}
	limaHome := filepath.Join(root, "lima")
	const limactl = "limactl"
	service := &app.Service{
		Driver:        lima.New(lima.Options{Home: limaHome, Limactl: limactl, Runner: runner}),
		Profiles:      profiles,
		Store:         store,
		GuestSSH:      ssh,
		DoctorHost:    doctor.Host{Gateway: doctor.DefaultGateway(runner), RequireGateway: runtime.GOOS == "linux"},
		HostCapacity:  hostinfo.System{},
		HostUsage:     hostinfo.ProcessUsage{Runner: runner},
		LocalHostName: ssh.LocalHostName,
		OpenURL: func(ctx context.Context, url string) error {
			opener := "open"
			if runtime.GOOS == "linux" {
				opener = "xdg-open"
			}
			_, err := runner.Run(ctx, execx.Cmd{Name: opener, Args: []string{url}})
			return err
		},
		Progress:  os.Stderr,
		CheckHost: checkHost,
	}
	if executable != nil {
		exe, err := executable()
		if err != nil {
			return nil, err
		}
		service.Isolation = &isolation.Host{Store: store, Executable: exe, LimaHome: limaHome, Limactl: limactl}
	}
	return service, nil
}

// NewDesktopService supplies the tool path missing from Finder launches.
func NewDesktopService(executable func() (string, error)) (*app.Service, error) {
	if runtime.GOOS == "darwin" {
		if err := os.Setenv("PATH", "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"); err != nil {
			return nil, err
		}
	}
	return NewService(executable)
}
