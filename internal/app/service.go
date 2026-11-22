// Package app coordinates box's host and guest operations.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Profiles provides guest definitions and their provisioning scripts.
type Profiles interface {
	Get(name string) (guest.Profile, error)
	Names() []string
	ReadScript(path string) ([]byte, error)
}

// SSH runs commands in guests using endpoints supplied by the VM driver.
type SSH interface {
	RunScript(ctx context.Context, ep vm.Endpoint, script []byte, env map[string]string, sudo bool, stdout, stderr io.Writer) error
	AuthorizeKeys(ctx context.Context, ep vm.Endpoint, keys []string) error
	Interactive(ep vm.Endpoint, argv []string) error
}

// Isolation sets up host-side network confinement for a VM's lifetime.
type Isolation interface {
	// Up runs before the VM starts. It returns how to launch the VM and what
	// the guest must be told. Up cleans up its own partial work on error.
	Up(ctx context.Context, box state.Box, instance vm.Instance) (Session, error)
	// Down runs after the VM stops or fails to start. It must be safe to retry.
	Down(ctx context.Context, name string) error
	// Running reports whether the VM's host-side services are up.
	Running(name string) bool
}

// Session describes the launch and guest network configuration.
type Session struct {
	Start    vm.StartOptions
	ProxyURL string // the guest's egress proxy, e.g. http://192.168.5.2:23001
	NoProxy  string
	DNS      string // the guest's resolver as "IP:PORT"
}

// NoIsolation is the temporary phase-zero implementation for fake or spike runs.
type NoIsolation struct{}

// Up returns an unwrapped launch with no guest network settings.
func (NoIsolation) Up(context.Context, state.Box, vm.Instance) (Session, error) {
	return Session{}, nil
}

// Down has no isolation resources to release.
func (NoIsolation) Down(context.Context, string) error { return nil }

// Running is always true: there is nothing to run.
func (NoIsolation) Running(string) bool { return true }

// DoctorHost provides the host controls for the guest's isolation diagnostics.
type DoctorHost interface {
	Probe(context.Context, string, int, bool) ([]string, []doctor.Check, func())
}

// Service implements the box use cases.
type Service struct {
	Driver        vm.Driver
	Profiles      Profiles
	Store         *state.Store
	GuestSSH      SSH
	Isolation     Isolation
	DoctorHost    DoctorHost
	LocalHostName func(context.Context) (string, error)
	Progress      io.Writer
}

func (s *Service) progress(format string, args ...any) {
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, format+"\n", args...)
	}
}

func (s *Service) load(name string) (state.Box, error) {
	box, err := s.Store.Load(name)
	if err == nil && box.Driver != s.Driver.Name() {
		err = fmt.Errorf("box %q uses driver %q, current driver is %q", name, box.Driver, s.Driver.Name())
	}
	return box, err
}

// Listing joins an on-disk record with driver status.
type Listing struct {
	state.Box
	Status vm.Status
}

// List returns the records and their operational status.
func (s *Service) List(ctx context.Context) ([]Listing, error) {
	boxes, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	instances, err := s.Driver.List(ctx)
	if err != nil {
		return nil, err
	}
	status := make(map[string]vm.Status, len(instances))
	for _, instance := range instances {
		status[instance.Name] = instance.Status
	}
	out := make([]Listing, 0, len(boxes))
	for _, box := range boxes {
		current := status[box.Name]
		if current == "" || box.Driver != s.Driver.Name() {
			current = vm.StatusBroken
		}
		out = append(out, Listing{Box: box, Status: current})
	}
	return out, nil
}

// SSH opens an interactive session or runs the requested remote command.
func (s *Service) SSH(ctx context.Context, name string, argv []string) error {
	if _, err := s.load(name); err != nil {
		return err
	}
	endpoint, err := s.Driver.SSH(ctx, name)
	if err != nil {
		return err
	}
	return s.GuestSSH.Interactive(endpoint, argv)
}

// SSHConfig renders laptop SSH aliases using an explicit or local mDNS hostname.
func (s *Service) SSHConfig(ctx context.Context, host string) (string, error) {
	boxes, err := s.Store.List()
	if err != nil || len(boxes) == 0 {
		return "", err
	}
	if host == "" {
		if s.LocalHostName == nil {
			return "", errors.New("local hostname resolver is unavailable; specify --host")
		}
		host, err = s.LocalHostName(ctx)
		if err != nil {
			return "", err
		}
	}
	if !configHost.MatchString(host) {
		return "", fmt.Errorf("invalid SSH host %q", host)
	}
	entries := make([]sshx.ConfigEntry, 0, len(boxes))
	for _, box := range boxes {
		if !guestUser.MatchString(box.User) {
			return "", fmt.Errorf("box %q has an invalid SSH user", box.Name)
		}
		entries = append(entries, sshx.ConfigEntry{Name: box.Name, Host: host, Port: box.SSHPort, User: box.User})
	}
	return sshx.RenderConfig(entries), nil
}
