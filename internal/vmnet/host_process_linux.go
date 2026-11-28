package vmnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func identifyHostProcess(pid int, written time.Time) (hostProcess, error) {
	start := processStart(pid)
	if start == 0 {
		return hostProcess{}, nil
	}
	var uptime unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &uptime); err != nil {
		return hostProcess{}, err
	}
	// Linux exposes process start time in USER_HZ units (100 per second).
	started := time.Now().Add(-time.Duration(uptime.Nano()) + time.Duration(start)*10*time.Millisecond)
	if started.After(written.Add(10 * time.Millisecond)) {
		return hostProcess{}, nil
	}
	return hostProcess{pid: pid, start: start}, nil
}

func watchHostProcess(parent context.Context, process hostProcess) (context.Context, func(), error) {
	descriptor, err := unix.PidfdOpen(process.pid, 0)
	switch {
	case errors.Is(err, unix.ESRCH):
		return nil, nil, nil
	case err != nil && !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL):
		return nil, nil, err
	}
	if !process.current() || (process.inode != 0 && !process.matchesDescriptor(descriptor)) {
		if descriptor >= 0 {
			unix.Close(descriptor)
		}
		return nil, nil, nil
	}

	ctx, cancel := context.WithCancel(parent)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer cancel()
		if descriptor < 0 {
			waitLinuxProcess(ctx, process)
			return
		}
		defer unix.Close(descriptor)
		events := []unix.PollFd{{Fd: int32(descriptor), Events: unix.POLLIN}}
		for ctx.Err() == nil {
			count, err := unix.Poll(events, 100)
			if count > 0 || (err != nil && !errors.Is(err, unix.EINTR)) {
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-exited }, nil
}

func waitLinuxProcess(ctx context.Context, process hostProcess) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !process.current() {
				return
			}
		}
	}
}

func processStart(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	// The parenthesized command can include spaces and parentheses.
	stat := string(data)
	fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return 0
	}
	start, _ := strconv.ParseUint(fields[19], 10, 64)
	return start
}

// resolveLaunchProcess reads the pidfs identity recorded by the QEMU wrapper.
// Each pidfs inode belongs to one process, even when a numeric PID is reused.
func resolveLaunchProcess(path string) (hostProcess, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return hostProcess{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return hostProcess{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64 {
		return hostProcess{}, fmt.Errorf("invalid QEMU launch PID file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64))
	if err != nil {
		return hostProcess{}, err
	}
	var p hostProcess
	if n, _ := fmt.Sscan(string(data), &p.pid, &p.device, &p.inode); n != 3 ||
		p.pid <= 1 || p.inode == 0 || !p.matchesPIDFD() {
		return hostProcess{}, nil
	}
	return p, nil
}

func (p hostProcess) matchesPIDFD() bool {
	fd, err := unix.PidfdOpen(p.pid, 0)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	return p.matchesDescriptor(fd)
}

func (p hostProcess) matchesDescriptor(fd int) bool {
	var stat unix.Stat_t
	return unix.Fstat(fd, &stat) == nil && stat.Dev == p.device && stat.Ino == p.inode
}
