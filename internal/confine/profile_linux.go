package confine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	linuxHostProfile   = "hostagent.json"
	linuxDaemonProfile = "vmnet.json"
	profileEnv         = "HOMEBOX_LINUX_PROFILE"
	qemuWrapper        = "homebox-qemu"
)

// The driver passes these output paths in Lima 2.3.0. Prepare their inodes
// before applying QEMU's per-file write rules.
var qemuOutputFiles = []string{"qemu.pid", "serial.log", "serialv.log"}

// Prepare writes the launch policy outside the VM process's writable paths.
func Prepare(rules Rules, dir string) ([]string, error) {
	if err := LinuxAvailable(); err != nil {
		return nil, err
	}
	var err error
	rules.RuntimeDir, err = filepath.EvalSymlinks(rules.RuntimeDir)
	if err != nil {
		return nil, err
	}
	name := linuxHostProfile
	if rules.Daemon {
		name = linuxDaemonProfile
		rules.Launcher = rules.Executable
	} else {
		tmp := filepath.Join(rules.RuntimeDir, "tmp")
		if err := os.MkdirAll(tmp, 0700); err != nil {
			return nil, err
		}
	}
	rules.Launcher, err = filepath.EvalSymlinks(rules.Launcher)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	data, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".landlock-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return nil, err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return nil, err
	}
	if !rules.Daemon {
		// Replace atomically so an existing symlink is never followed on update.
		link := f.Name() + ".link"
		if err := os.Symlink(rules.Launcher, link); err != nil {
			return nil, err
		}
		defer os.Remove(link)
		if err := os.Rename(link, filepath.Join(dir, qemuWrapper)); err != nil {
			return nil, err
		}
	}
	return []string{rules.Launcher, "_confine", path}, nil
}

// LinuxAvailable checks the kernel interfaces used by the Linux launcher.
func LinuxAvailable() error {
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("Linux host confinement requires x86-64")
	}
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || abi < 6 {
		return fmt.Errorf("Linux host confinement requires enabled Landlock ABI 6 or later (ABI %d, error %v)", int(abi), errno)
	}
	return nil
}

func dispatch(args []string) (bool, error) {
	if filepath.Base(args[0]) == qemuWrapper {
		return true, launchQEMU(os.Getenv(profileEnv), args[1:])
	}
	if len(args) < 2 || args[1] != "_confine" {
		return false, nil
	}
	if len(args) < 4 {
		return true, fmt.Errorf("expected a profile and command")
	}
	rules, err := readRules(args[2])
	if err != nil {
		return true, err
	}
	if args[3] == "probe" {
		return true, probe(rules, args[2], args[4:])
	}
	if err := install(rules, args[2], false); err != nil {
		return true, err
	}
	env := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + rules.Home, "LANG=C.UTF-8"}
	if !rules.Daemon {
		env = append(env, "LIMA_HOME="+rules.LimaHome, "XDG_CONFIG_HOME="+filepath.Join(rules.LimaHome, "_config"), "TMPDIR="+filepath.Join(rules.RuntimeDir, "tmp"),
			"QEMU_SYSTEM_X86_64="+filepath.Join(filepath.Dir(args[2]), qemuWrapper), profileEnv+"="+args[2])
	}
	return true, unix.Exec(rules.Executable, append([]string{rules.Executable}, args[4:]...), env)
}

func readRules(path string) (Rules, error) {
	var rules Rules
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &rules)
	}
	return rules, err
}

func launchQEMU(path string, args []string) error {
	rules, err := readRules(path)
	if err != nil {
		return err
	}
	if err := restrictQEMU(args, rules); err != nil {
		return err
	}
	launching := slices.Contains(args, "-pidfile")
	if launching {
		for _, name := range qemuOutputFiles {
			path := filepath.Join(rules.RuntimeDir, name)
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
			if err != nil {
				return fmt.Errorf("prepare QEMU output file: %w", err)
			}
			info, statErr := file.Stat()
			closeErr := file.Close()
			if err := errors.Join(statErr, closeErr); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("QEMU output must be a regular file: %s", name)
			}
		}
	}
	if err := install(rules, path, true); err != nil {
		return err
	}
	if launching {
		if err := publishQEMUPID(rules.HostPIDFile); err != nil {
			return fmt.Errorf("publish QEMU launch PID: %w", err)
		}
	}
	return unix.Exec("/usr/bin/qemu-system-x86_64", append([]string{"qemu-system-x86_64"}, args...),
		[]string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HOME=" + rules.RuntimeDir})
}

func restrictQEMU(args []string, rules Rules) error {
	ssh := rules.ConnectTCP
	// Lima probes these capabilities with standalone commands before launch.
	switch strings.Join(args, "\x00") {
	case "--version", "-M\x00none\x00-accel\x00help", "-M\x00none\x00-netdev\x00help", "-machine\x00help":
		return nil
	case "-cpu\x00help\x00-machine\x00q35", "-cpu\x00help\x00-machine\x00pc":
		return nil
	}
	if len(ssh) != 1 {
		return fmt.Errorf("QEMU requires a single configured SSH port")
	}
	foundUser, foundGateway := false, false
	for i, arg := range args {
		if arg == "-nic" || arg == "-net" || strings.HasPrefix(arg, "-netdev=") {
			return fmt.Errorf("unsupported QEMU network argument %q", arg)
		}
		if arg != "-netdev" || i+1 >= len(args) {
			continue
		}
		next := args[i+1]
		want := qemuUserNetdev(ssh[0])
		switch {
		case !foundUser && next == want:
			args[i+1] = RestrictedQEMUSLIRP(ssh[0])
			foundUser = true
		case !foundGateway && strings.HasPrefix(next, "socket,id=net1,fd="):
			fd, err := strconv.Atoi(strings.TrimPrefix(next, "socket,id=net1,fd="))
			if err != nil || fd < 3 {
				return fmt.Errorf("QEMU gateway must use an inherited socket")
			}
			peer, err := unix.Getpeername(fd)
			socket, ok := peer.(*unix.SockaddrUnix)
			if err != nil || !ok || socket.Name != rules.GatewaySocket {
				return fmt.Errorf("QEMU did not receive the configured gateway socket")
			}
			foundGateway = true
		default:
			return fmt.Errorf("unexpected QEMU network backend")
		}
	}
	if !foundUser || !foundGateway {
		return fmt.Errorf("expected Lima's SSH and Homebox gateway network backends")
	}
	return nil
}

func publishQEMUPID(path string) error {
	pid := os.Getpid()
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	return os.WriteFile(path, fmt.Appendf(nil, "%d %d %d\n", pid, stat.Dev, stat.Ino), 0600)
}

func qemuUserNetdev(port int) string {
	return fmt.Sprintf("user,id=net0,net=192.168.5.0/24,dhcpstart=192.168.5.15,hostfwd=tcp:127.0.0.1:%d-:22", port)
}

// RestrictedQEMUSLIRP is the exact SSH-only backend expected by Linux doctor.
func RestrictedQEMUSLIRP(port int) string {
	// QEMU 10.2 requires an absolute path and treats a dash as the end
	// of the path. Lima starts QEMU in the instance directory.
	return qemuUserNetdev(port) + ",restrict=on,ipv6=off,hostfwd=unix:/proc/self/cwd/" + QEMUSSHSocket + "-:22"
}
