package vmnet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func watchHostProcess(parent context.Context, pid int, written time.Time) (context.Context, func(), error) {
	descriptor, err := unix.PidfdOpen(pid, 0)
	switch {
	case errors.Is(err, unix.ESRCH):
		return nil, nil, nil
	case err != nil && !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL):
		return nil, nil, err
	}
	start, alive := linuxProcessStart(pid)
	var uptime unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &uptime); err != nil {
		if descriptor >= 0 {
			unix.Close(descriptor)
		}
		return nil, nil, err
	}
	// Linux exposes process start time in USER_HZ units (100 per second).
	started := time.Now().Add(-time.Duration(uptime.Nano()) + time.Duration(start)*10*time.Millisecond)
	if !alive || started.After(written.Add(10*time.Millisecond)) {
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
			waitLinuxProcess(ctx, pid, start)
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

func waitLinuxProcess(ctx context.Context, pid int, start uint64) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, alive := linuxProcessStart(pid)
			if !alive || start != current {
				return
			}
		}
	}
}

func linuxProcessStart(pid int) (uint64, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}
	// The parenthesized command can include spaces and parentheses.
	stat := string(data)
	fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return 0, false
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	return start, err == nil
}
