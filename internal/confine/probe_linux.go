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
			out, runErr := exec.CommandContext(probeCtx, rules.Launcher, "_confine", path, "probe", name, f.Name(), port, abstract).CombinedOutput()
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
	if len(args) != 4 {
		return errors.New("expected profile kind, file, TCP port and Unix canary")
	}
	port, err := strconv.Atoi(args[2])
	if err != nil || !validPort(port) {
		return errors.New("invalid TCP canary port")
	}
	if err := install(rules, path, args[0] == "qemu"); err != nil {
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
	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err == nil {
		unix.Close(fd)
	}
	if !errors.Is(err, unix.EPERM) {
		return fmt.Errorf("IPv6 socket: expected EPERM, got %v", err)
	}
	fd, err = unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	err = unix.Connect(fd, &unix.SockaddrUnix{Name: args[3]})
	unix.Close(fd)
	if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
		return fmt.Errorf("abstract Unix connect: expected permission denial, got %v", err)
	}
	if rules.Daemon {
		return nil
	}
	if err := checkFastOpenFilter(false); err != nil {
		return err
	}
	fd, err = unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	err = unix.Connect(fd, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
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
func checkFastOpenFilter(daemon bool) error {
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
