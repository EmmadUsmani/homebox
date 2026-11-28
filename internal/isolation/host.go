// Package isolation confines a VM to the host-side services box runs for it.
//
// It starts the socket gateway and SSH relay and confines the driver's host process.
// It implements app.Isolation.
package isolation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

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
	if runtime.GOOS == "linux" && instance.SSHPIDFile == "" {
		return app.Session{}, fmt.Errorf("Linux isolation requires the Lima QEMU driver")
	}
	if instance.SSHPIDFile != "" {
		instance.HostPIDFile = instance.SSHPIDFile
	}
	if !filepath.IsAbs(instance.HostPIDFile) {
		return app.Session{}, fmt.Errorf("VM driver did not report an absolute host process PID path")
	}
	pidDir, err := filepath.EvalSymlinks(filepath.Dir(instance.HostPIDFile))
	if err != nil {
		return app.Session{}, err
	}
	lan := host.LAN && !box.ImageBuild
	daemon := daemonConfig(box, dir, lan)
	daemon.HostPIDFile = filepath.Join(pidDir, filepath.Base(instance.HostPIDFile))
	hostRules := rules(box, instance)
	if runtime.GOOS == "linux" {
		daemon.SSHTargetSocket = filepath.Join(pidDir, confine.QEMUSSHSocket)
		// Keep the launch identity outside both writable runtime directories.
		// Replace the inode before granting QEMU an exact-file write rule.
		daemon.HostPIDFile = filepath.Join(h.Store.BoxDir(box.Name), "qemu-launch.pid")
		file, err := os.CreateTemp(h.Store.BoxDir(box.Name), ".qemu-launch-")
		if err != nil {
			return app.Session{}, err
		}
		defer os.Remove(file.Name())
		if err := file.Close(); err != nil {
			return app.Session{}, err
		}
		if err := os.Rename(file.Name(), daemon.HostPIDFile); err != nil {
			return app.Session{}, err
		}
	}
	hostRules.HostPIDFile = daemon.HostPIDFile
	hostRules.GatewaySocket = daemon.Socket
	hostRules.Home = home
	hostRules.LimaHome = h.LimaHome
	hostRules.Executable = limactl
	hostRules.Launcher = h.Executable
	wrap, err := confine.Prepare(hostRules, h.Store.BoxDir(box.Name))
	if err != nil {
		return app.Session{}, err
	}

	executable, err := filepath.EvalSymlinks(h.Executable)
	if err != nil {
		return app.Session{}, err
	}
	daemonWrap, err := confine.Prepare(confine.Rules{
		Daemon: true, RuntimeDir: dir, Executable: executable,
		GatewaySocket: daemon.Socket, HostPIDFile: daemon.HostPIDFile,
		BindTCP: []confine.Bind{{Port: daemon.SSHPublish.Port, LAN: lan}},
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

	return app.Session{Start: vm.StartOptions{
		Wrap:   wrap,
		Socket: vm.SocketAttachment{Path: daemon.Socket, Interface: "lima0", MAC: vmnet.GuestMAC(), RouteMetric: 100},
		DNS:    []string{"9.9.9.9", "1.1.1.1"},
	}}, nil
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
	return h.Store.RunDir(name)
}

func (h *Host) prepareRunDir(name string) (string, error) {
	if err := state.ValidateName(name); err != nil {
		return "", err
	}
	dir := h.runDir(name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", fmt.Errorf("VM network directory must have mode 0700: %s", dir)
	}
	return filepath.EvalSymlinks(dir)
}

func daemonConfig(box state.Box, dir string, lan bool) vmnet.Config {
	return vmnet.Config{
		Name:       box.Name,
		Socket:     filepath.Join(dir, "net.sock"),
		SSHPublish: vmnet.Endpoint{IP: state.HostIP(lan), Port: box.SSHPort},
		SSHTarget:  state.DriverSSHPort(box.Index),
		LogFile:    state.NetworkLogPath(dir),
		PIDFile:    pidFile(dir),
	}
}

// rules allows SSH and the box's configured inbound HTTP forwards.
func rules(box state.Box, instance vm.Instance) confine.Rules {
	sshTarget := state.DriverSSHPort(box.Index)
	binds := []confine.Bind{{Port: sshTarget}}
	for _, forward := range box.Forwards {
		binds = append(binds, confine.Bind{Port: forward.HostPort, LAN: forward.HostIP != "127.0.0.1"})
	}
	return confine.Rules{
		RuntimeDir: instance.RuntimeDir,
		ConnectTCP: []int{sshTarget},
		BindTCP:    binds,
	}
}
