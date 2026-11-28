package confine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// ProbeLinux checks each launch policy with controlled file and socket attempts.
func ProbeLinux(ctx context.Context, dir string) map[string]error {
	results := make(map[string]error)
	home, err := os.UserHomeDir()
	if err != nil {
		results["setup"] = err
		return results
	}
	f, err := os.CreateTemp(home, ".homebox-doctor-")
	if err != nil {
		results["setup"] = err
		return results
	}
	f.Close()
	defer os.Remove(f.Name())
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		results["setup"] = err
		return results
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	abstract := "@homebox-doctor-" + filepath.Base(f.Name())
	socket, err := net.Listen("unix", abstract)
	if err != nil {
		results["setup"] = err
		return results
	}
	defer socket.Close()
	pathname := filepath.Join(home, filepath.Base(f.Name())+".sock")
	pathSocket, err := net.Listen("unix", pathname)
	if err != nil {
		results["setup"] = err
		return results
	}
	defer pathSocket.Close()
	for _, name := range []string{"hostagent", "qemu", "vmnet"} {
		profile := linuxHostProfile
		if name == "vmnet" {
			profile = linuxDaemonProfile
		}
		path := filepath.Join(dir, profile)
		rules, err := readRules(path)
		if err == nil && rules.Daemon != (name == "vmnet") {
			err = errors.New("wrong confinement profile type")
		}
		if err == nil {
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			out, runErr := exec.CommandContext(probeCtx, rules.Launcher, "_confine", path, "probe", name, f.Name(), port, abstract, pathname).CombinedOutput()
			cancel()
			err = runErr
			if err != nil {
				err = fmt.Errorf("%w: %s", err, out)
			}
		}
		results[name] = err
	}
	return results
}

func probe(rules Rules, path string, args []string) error {
	if len(args) != 5 {
		return errors.New("expected profile kind, file, TCP port and Unix canaries")
	}
	port, err := strconv.Atoi(args[2])
	if err != nil || !validPort(port) {
		return errors.New("invalid TCP canary port")
	}
	qemu := args[0] == "qemu"
	if err := install(rules, path, qemu); err != nil {
		return err
	}
	for _, mode := range []int{os.O_RDONLY, os.O_WRONLY} {
		f, err := os.OpenFile(args[1], mode, 0)
		if err == nil {
			f.Close()
		}
		if !errors.Is(err, unix.EACCES) {
			return fmt.Errorf("outside file access mode %d: expected EACCES, got %v", mode, err)
		}
	}
	if err := checkSocketFilter(rules.Daemon, qemu); err != nil {
		return err
	}
	parent := os.Getppid()
	if err := unix.Kill(parent, 0); !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("signal to parent: expected EPERM, got %v", err)
	}
	f, err := os.Open(fmt.Sprintf("/proc/%d/cmdline", parent))
	if err == nil {
		f.Close()
	}
	if !errors.Is(err, unix.EACCES) {
		return fmt.Errorf("parent cmdline: expected EACCES, got %v", err)
	}
	for _, socket := range []struct {
		name    string
		address string
		denied  bool
	}{
		{"abstract Unix", args[3], true},
		{"pathname Unix", args[4], qemu},
	} {
		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return err
		}
		err = unix.Connect(fd, &unix.SockaddrUnix{Name: socket.address})
		unix.Close(fd)
		if socket.denied && !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
			return fmt.Errorf("%s connect: expected permission denial, got %v", socket.name, err)
		}
		if !socket.denied && err != nil {
			return fmt.Errorf("%s connect: %w", socket.name, err)
		}
	}
	if err := checkTCPConnect(port, rules.Daemon); err != nil {
		return err
	}
	// Successful exec replaces the probe and exits nonzero.
	if err := unix.Exec("/usr/bin/false", []string{"false"}, nil); !errors.Is(err, unix.EACCES) {
		return fmt.Errorf("exec outside allowlist: expected EACCES, got %v", err)
	}
	return nil
}

func checkTCPConnect(port int, daemon bool) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	err = unix.Connect(fd, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
	if daemon {
		if err != nil {
			return fmt.Errorf("gateway TCP connect: %w", err)
		}
		return nil
	}
	if !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("TCP connect: expected permission denial, got %v", err)
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT, 1); err != nil {
		return fmt.Errorf("set TCP_FASTOPEN_CONNECT for probe: %w", err)
	}
	err = unix.Connect(fd, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
	if !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("TCP_FASTOPEN_CONNECT: expected permission denial, got %v", err)
	}
	return nil
}

// An invalid descriptor makes the errno distinguish a seccomp denial from
// normal kernel handling, without depending on network or TCP settings.
func checkSocketFilter(daemon, qemu bool) error {
	for _, call := range []struct {
		name string
		nr   uintptr
	}{
		{"io_uring_setup", unix.SYS_IO_URING_SETUP},
		{"io_uring_enter", unix.SYS_IO_URING_ENTER},
		{"io_uring_register", unix.SYS_IO_URING_REGISTER},
		{"ptrace", unix.SYS_PTRACE},
		{"process_vm_readv", unix.SYS_PROCESS_VM_READV},
		{"process_vm_writev", unix.SYS_PROCESS_VM_WRITEV},
	} {
		_, _, err := unix.Syscall6(call.nr, ^uintptr(0), 0, 0, 0, 0, 0)
		if err != unix.EPERM {
			return fmt.Errorf("%s filter: expected EPERM, got %v", call.name, err)
		}
	}
	for _, socket := range []struct {
		family, kind, protocol int
		allowed                bool
	}{
		{unix.AF_INET6, unix.SOCK_STREAM, 0, false},
		{unix.AF_PACKET, unix.SOCK_RAW, 0, false},
		{unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE, !qemu},
		{unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_USERSOCK, false},
		{unix.AF_INET, unix.SOCK_STREAM, 0, true},
		{unix.AF_INET, unix.SOCK_DGRAM, 0, daemon},
		{unix.AF_UNIX, unix.SOCK_DGRAM, 0, !qemu},
	} {
		fd, err := unix.Socket(socket.family, socket.kind|unix.SOCK_CLOEXEC, socket.protocol)
		if err == nil {
			unix.Close(fd)
		}
		if socket.allowed && err != nil || !socket.allowed && !errors.Is(err, unix.EPERM) {
			return fmt.Errorf("socket (%d, %d, %d), allowed=%t: got %v", socket.family, socket.kind, socket.protocol, socket.allowed, err)
		}
	}
	if qemu {
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
		if err == nil {
			unix.Close(fds[0])
			unix.Close(fds[1])
		}
		if !errors.Is(err, unix.EPERM) {
			return fmt.Errorf("QEMU datagram socketpair: expected EPERM, got %v", err)
		}
	}
	return checkSendFilter(daemon)
}

func checkSendFilter(daemon bool) error {
	const invalidFD = ^uintptr(0)
	blocked := unix.EPERM
	if daemon {
		blocked = unix.EBADF
	}
	const fastOpen = unix.MSG_FASTOPEN | unix.MSG_DONTWAIT
	for _, call := range []struct {
		name string
		nr   uintptr
		arg2 uintptr
		arg3 uintptr
		want unix.Errno
	}{
		{"sendto Fast Open", unix.SYS_SENDTO, 0, fastOpen, blocked},
		{"sendmsg Fast Open", unix.SYS_SENDMSG, fastOpen, 0, blocked},
		{"sendmmsg", unix.SYS_SENDMMSG, 1, 0, blocked},
		{"ordinary sendto", unix.SYS_SENDTO, 0, unix.MSG_DONTWAIT, unix.EBADF},
		{"ordinary sendmsg", unix.SYS_SENDMSG, unix.MSG_DONTWAIT, 0, unix.EBADF},
	} {
		_, _, err := unix.Syscall6(call.nr, invalidFD, 0, call.arg2, call.arg3, 0, 0)
		if err != call.want {
			return fmt.Errorf("%s filter: expected %v, got %v", call.name, call.want, err)
		}
	}
	return nil
}
