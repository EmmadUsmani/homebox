// Package isolation confines a VM to the host-side services box runs for it.
//
// For each VM it starts the network daemon (egress proxy, DNS forwarder and
// SSH relay; see internal/vmnet) and confines the driver's host process so
// those loopback ports are the only things the VM can reach (internal/confine).
// It implements app.Isolation.
package isolation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/confine"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/EmmadUsmani/homebox/internal/vmnet"
)

// DaemonCommand is the hidden box subcommand that runs a VM's network daemon.
const DaemonCommand = "_vmnet"

// Host is the isolation for VMs on this machine.
type Host struct {
	Store *state.Store
	// Executable is the box binary to run the daemon from (os.Executable()).
	Executable string
	LimaHome   string
	Limactl    string
}

var _ app.Isolation = (*Host)(nil)

// Up starts the VM's network daemon and returns the confined launch command.
func (h *Host) Up(_ context.Context, box state.Box, instance vm.Instance) (app.Session, error) {
	if instance.HostAddress == "" {
		return app.Session{}, errors.New("the VM driver reports no host address for the guest")
	}
	host, err := h.Store.LoadHost()
	if err != nil {
		return app.Session{}, err
	}
	dir, err := h.prepareRunDir(box.Name)
	if err != nil {
		return app.Session{}, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return app.Session{}, err
	}
	limactl, err := exec.LookPath(h.Limactl)
	if err != nil {
		return app.Session{}, err
	}
	hostRules := rules(box, instance)
	hostRules.Home = home
	hostRules.LimaHome = h.LimaHome
	hostRules.Executable = limactl
	wrap, err := confine.Prepare(hostRules, h.Store.BoxDir(box.Name))
	if err != nil {
		return app.Session{}, err
	}

	daemon := daemonConfig(box, dir, host.LAN)
	executable, err := filepath.EvalSymlinks(h.Executable)
	if err != nil {
		return app.Session{}, err
	}
	daemonWrap, err := confine.Prepare(confine.Rules{
		Daemon: true, RuntimeDir: dir, Executable: executable,
		BindTCP: []confine.Bind{{Port: daemon.ProxyPort}, {Port: daemon.DNSPort}, {
			Port: daemon.SSHPublish.Port, LAN: host.LAN,
		}},
		BindUDP: []confine.Bind{{Port: daemon.DNSPort}},
	}, h.Store.BoxDir(box.Name))
	if err != nil {
		return app.Session{}, err
	}
	// Replace any daemon left over from a VM that stopped on its own.
	if err := vmnet.Stop(daemon.PIDFile); err != nil {
		return app.Session{}, fmt.Errorf("stop old network daemon: %w", err)
	}
	if _, err := vmnet.Spawn(daemonWrap[0], append(daemonWrap[1:], executable, DaemonCommand), daemon); err != nil {
		return app.Session{}, fmt.Errorf("start network daemon (log: %s): %w", daemon.LogFile, err)
	}

	return app.Session{
		Start:    vm.StartOptions{Wrap: wrap},
		ProxyURL: "http://" + net.JoinHostPort(instance.HostAddress, strconv.Itoa(daemon.ProxyPort)),
		NoProxy:  "localhost,127.0.0.1,::1",
		DNS:      net.JoinHostPort(instance.HostAddress, strconv.Itoa(daemon.DNSPort)),
	}, nil
}

// Down stops the VM's network daemon. It is safe to call when none runs.
func (h *Host) Down(_ context.Context, name string) error {
	dir, err := h.prepareRunDir(name)
	if err != nil {
		return err
	}
	return vmnet.Stop(pidFile(dir))
}

// Running reports whether name's network daemon holds its PID file.
func (h *Host) Running(name string) bool {
	if state.ValidateName(name) != nil {
		return false
	}
	_, running := vmnet.Running(pidFile(h.runDir(name)))
	return running
}

func pidFile(dir string) string {
	return filepath.Join(dir, "vmnet.pid")
}

func (h *Host) runDir(name string) string {
	return filepath.Join(h.Store.BoxDir(name), "run")
}

func (h *Host) prepareRunDir(name string) (string, error) {
	if err := state.ValidateName(name); err != nil {
		return "", err
	}
	dir := h.runDir(name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(dir)
}

func daemonConfig(box state.Box, dir string, lan bool) vmnet.Config {
	return vmnet.Config{
		Name:       box.Name,
		ProxyPort:  state.ProxyPort(box.Index),
		DNSPort:    state.DNSPort(box.Index),
		SSHPublish: vmnet.Endpoint{IP: state.HostIP(lan), Port: box.SSHPort},
		SSHTarget:  state.DriverSSHPort(box.Index),
		LogFile:    filepath.Join(dir, "vmnet.log"),
		PIDFile:    pidFile(dir),
	}
}

// rules lets the VM's host process reach only its own SSH forward, proxy and
// DNS forwarder (UDP, and TCP for truncated answers), and listen only on its
// SSH forward and published HTTP ports.
func rules(box state.Box, instance vm.Instance) confine.Rules {
	sshTarget := state.DriverSSHPort(box.Index)
	binds := []confine.Bind{{Port: sshTarget}}
	for _, forward := range box.Forwards {
		binds = append(binds, confine.Bind{Port: forward.HostPort, LAN: forward.HostIP != "127.0.0.1"})
	}
	return confine.Rules{
		RuntimeDir: instance.RuntimeDir,
		ConnectTCP: []int{sshTarget, state.ProxyPort(box.Index), state.DNSPort(box.Index)},
		ConnectUDP: []int{state.DNSPort(box.Index)},
		BindTCP:    binds,
	}
}
