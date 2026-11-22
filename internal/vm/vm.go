// Package vm defines the boundary between box and a VM layer such as Lima.
//
// Everything box knows about running a VM goes through Driver. A driver turns
// a Spec into a running machine and reports how to reach it over SSH; it knows
// nothing about guest profiles, port allocation or egress policy. Those live
// in other packages and reach the driver only as plain values.
package vm

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a named VM does not exist in the driver.
var ErrNotFound = errors.New("vm not found")

// Status is the coarse lifecycle state of a VM.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusBroken  Status = "broken" // exists, but the driver can't tell or can't use it
)

// Image is a bootable disk image for one CPU architecture.
type Image struct {
	Arch   string // "aarch64" or "x86_64"
	URL    string
	Digest string // "sha256:..." or "sha512:..."; empty means unchecked
}

// PortForward publishes a guest TCP port on the host.
type PortForward struct {
	GuestPort int
	HostPort  int
	HostIP    string // "127.0.0.1", or "0.0.0.0" when the host publishes on the LAN
}

// Spec is everything a driver needs to create a VM.
type Spec struct {
	Name      string
	Images    []Image // the driver picks the one matching the host's arch
	CPUs      int
	MemoryGiB int
	DiskGiB   int
	User      string        // admin user created in the guest
	SSHPort   int           // host loopback port the driver binds for the guest's sshd
	Forwards  []PortForward // published ports other than SSH
	// DNS, when set, replaces the guest's resolvers (e.g. a host-side resolver
	// reachable through the VM's network). Empty keeps the driver's default.
	DNS []string
}

// Endpoint is how to reach a VM's sshd from the host.
type Endpoint struct {
	Host         string // usually 127.0.0.1
	Port         int
	User         string
	IdentityFile string // private key the driver authorized in the guest
	KnownHosts   string // file with the guest's host keys, if the driver keeps one
}

// Instance is a VM as the driver sees it.
type Instance struct {
	Name   string
	Status Status
	// RuntimeDir is the driver's per-VM state directory on the host. The host
	// confinement needs it, because the VM's host process talks over unix
	// sockets kept there.
	RuntimeDir string
}

// StartOptions controls how the VM's host-side process is launched.
type StartOptions struct {
	// Wrap, when non-empty, is a command prefix the driver runs its start
	// command under, e.g. ["sandbox-exec", "-f", profile]. Host confinement
	// uses it so the VM's network process inherits the host's egress rules.
	Wrap []string
}

// Driver is a VM layer: Lima today, perhaps Incus on Linux later.
type Driver interface {
	// Name identifies the driver, e.g. "lima".
	Name() string
	// Create makes a new, stopped VM. It may download the image.
	Create(ctx context.Context, spec Spec) error
	Start(ctx context.Context, name string, opts StartOptions) error
	Stop(ctx context.Context, name string) error
	// Delete removes the VM and its disks. Deleting a missing VM is not an error.
	Delete(ctx context.Context, name string) error
	// Get returns ErrNotFound when the VM doesn't exist.
	Get(ctx context.Context, name string) (Instance, error)
	List(ctx context.Context) ([]Instance, error)
	// SSH reports how to reach a running VM's sshd from the host.
	SSH(ctx context.Context, name string) (Endpoint, error)
	// SetForwards replaces a stopped VM's published ports; they apply at its next start.
	SetForwards(ctx context.Context, name string, forwards []PortForward) error
}
