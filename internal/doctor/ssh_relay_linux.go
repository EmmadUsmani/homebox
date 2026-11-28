package doctor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/confine"
	"github.com/EmmadUsmani/homebox/internal/vmnet"
	"golang.org/x/sys/unix"
)

// ProbeLinuxSSHRelay checks the same Unix forward used by the published relay.
func (Host) ProbeLinuxSSHRelay(ctx context.Context, pidFile, runtimeDir, boxDir string, port int) []Check {
	targetOwner := Check{Name: "ssh-relay-target-owner",
		OK:     vmnet.OwnsUnixSSHTarget(ctx, filepath.Join(boxDir, confine.QEMULaunchPIDFile), filepath.Join(runtimeDir, confine.QEMUSSHSocket)),
		Detail: "could not authenticate the QEMU Unix SSH listener"}
	tcpOwner := Check{Name: "ssh-driver-target-owner", OK: vmnet.OwnsSSHTarget(pidFile, port),
		Detail: "could not verify the QEMU loopback SSH listener"}
	slirp := Check{Name: "qemu-slirp-restricted", Detail: "could not confirm QEMU's restricted SLIRP and inherited gateway socket"}
	data, err := readLinuxPIDFile(pidFile)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid > 1 && targetOwner.OK && tcpOwner.OK {
			args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
			status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
			slirp.OK = err == nil && statusErr == nil &&
				confine.CheckRunningQEMUNetwork(strings.Split(string(args), "\x00"), port, pid) == nil &&
				bytes.Contains(status, []byte("NoNewPrivs:\t1\n")) && bytes.Contains(status, []byte("Seccomp:\t2\n")) &&
				vmnet.OwnsSSHTarget(pidFile, port)
		}
	}
	return []Check{targetOwner, tcpOwner, slirp}
}

func readLinuxPIDFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32 {
		return nil, fmt.Errorf("invalid PID file")
	}
	return io.ReadAll(io.LimitReader(f, 32))
}
