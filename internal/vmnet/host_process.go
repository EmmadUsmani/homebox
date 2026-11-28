//go:build darwin || linux

package vmnet

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/EmmadUsmani/homebox/internal/relay"
)

type hostProcess struct {
	pid    int
	start  uint64
	device uint64
	inode  uint64
}

func resolveHostProcess(pidFile string) (hostProcess, error) {
	pid, written, err := readHostPID(pidFile)
	if err != nil || pid <= 1 {
		return hostProcess{}, err
	}
	return identifyHostProcess(pid, written)
}

// OwnsSSHTarget checks the driver SSH listener without opening a connection to it.
func OwnsSSHTarget(pidFile string, port int) bool {
	process, err := resolveHostProcess(pidFile)
	return err == nil && port > 0 && port <= 65535 &&
		process.listenerID(netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(port))) != 0
}

func (p hostProcess) current() bool {
	if p.inode != 0 {
		return p.matchesPIDFD()
	}
	return p.pid > 1 && p.start != 0 && processStart(p.pid) == p.start
}

func (p hostProcess) listenerID(target netip.AddrPort) uint64 {
	if !p.current() {
		return 0
	}
	id := tcpListenerID(p.pid, target)
	if id == 0 || !p.current() {
		return 0
	}
	return id
}

func serveRelay(ctx context.Context, ssh relay.Relay, pidFile, targetSocket string, logger *slog.Logger) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		resolve := resolveHostProcess
		if targetSocket != "" {
			resolve = resolveLaunchProcess
		}
		process, err := resolve(pidFile)
		if err == nil && process.pid != 0 {
			lifetime, done, err := watchHostProcess(ctx, process)
			if err != nil {
				return err
			}
			if lifetime != nil {
				warnings := rate.NewLimiter(rate.Every(time.Minute), 1)
				if targetSocket != "" {
					ssh.TargetDial = process.unixDial(targetSocket)
				} else {
					ssh.TargetListenerID = process.listenerID
				}
				ssh.TargetRefused = func(target netip.AddrPort) {
					if warnings.Allow() {
						logger.Warn("SSH relay refused unverified target owner", "target", target.String())
					}
				}
				err := ssh.Run(lifetime)
				done()
				return err
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

func readHostPID(path string) (int, time.Time, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, time.Time{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32 {
		return 0, time.Time{}, errors.New("invalid host PID file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 32))
	if err != nil {
		return 0, time.Time{}, err
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32)
	if err != nil {
		return 0, info.ModTime(), nil
	}
	return int(pid), info.ModTime(), nil
}
