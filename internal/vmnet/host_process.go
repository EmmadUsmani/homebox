//go:build darwin || linux

package vmnet

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/EmmadUsmani/homebox/internal/relay"
)

func serveRelay(ctx context.Context, ssh relay.Relay, pidFile string) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		pid, written, err := readHostPID(pidFile)
		if err == nil && pid > 1 {
			lifetime, done, err := watchHostProcess(ctx, pid, written)
			if err != nil {
				return err
			}
			if lifetime != nil {
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
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, info.ModTime(), nil
}
