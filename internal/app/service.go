// Package app coordinates box's host and guest operations.
package app

import (
	"context"
	"fmt"
	"io"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/hostinfo"
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
	HostKeys(ctx context.Context, ep vm.Endpoint) ([]string, error)
	RunScript(ctx context.Context, ep vm.Endpoint, script []byte, env map[string]string, sudo bool, stdout, stderr io.Writer) error
	AuthorizeKeys(ctx context.Context, ep vm.Endpoint, keys []string) error
	Stream(ctx context.Context, ep vm.Endpoint, argv []string, stdout, stderr io.Writer) error
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

// HostCapacity reports available CPUs and total RAM. Zero memory means unknown.
type HostCapacity interface {
	Capacity() (cpus int, memoryBytes uint64, err error)
}

// HostUsage samples host processes for a batch of running driver instances.
type HostUsage interface {
	Sample(context.Context, []vm.Instance) map[string]hostinfo.UsageSample
}

// Service implements the box use cases.
type Service struct {
	Driver        vm.Driver
	Profiles      Profiles
	Store         *state.Store
	GuestSSH      SSH
	Isolation     Isolation
	DoctorHost    DoctorHost
	HostCapacity  HostCapacity
	HostUsage     HostUsage
	LocalHostName func(context.Context) (string, error)
	OpenURL       func(context.Context, string) error
	Progress      io.Writer
}

func (s *Service) progress(format string, args ...any) {
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, format+"\n", args...)
	}
}

func (s *Service) load(name string) (state.Box, error) {
	box, err := s.loadRecord(name)
	if err == nil && box.PendingResize != nil {
		err = pendingResizeError(name)
	}
	return box, err
}

func (s *Service) loadRecord(name string) (state.Box, error) {
	box, err := s.Store.Load(name)
	if err == nil && box.Driver != s.Driver.Name() {
		err = fmt.Errorf("box %q uses driver %q, current driver is %q", name, box.Driver, s.Driver.Name())
	}
	return box, err
}

// Listing joins an on-disk record with driver status.
type Listing struct {
	state.Box
	Status   vm.Status
	instance vm.Instance
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
	byName := make(map[string]vm.Instance, len(instances))
	for _, instance := range instances {
		byName[instance.Name] = instance
	}
	out := make([]Listing, 0, len(boxes))
	for _, box := range boxes {
		if box.PendingResize != nil {
			return nil, pendingResizeError(box.Name)
		}
		instance := byName[box.Name]
		current := instance.Status
		if current == "" || box.Driver != s.Driver.Name() {
			current = vm.StatusBroken
		}
		out = append(out, Listing{Box: box, Status: current, instance: instance})
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
