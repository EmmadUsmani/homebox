package vmnet

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func processStart(pid int) uint64 {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info.Proc.P_pid != int32(pid) || info.Proc.P_stat == 5 { // SZOMB
		return 0
	}
	start := info.Proc.P_starttime
	return uint64(start.Sec)*1_000_000 + uint64(start.Usec)
}

func identifyHostProcess(pid int, written time.Time) (hostProcess, error) {
	start := processStart(pid)
	if start == 0 || time.UnixMicro(int64(start)).After(written) {
		return hostProcess{}, nil
	}
	return hostProcess{pid: pid, start: start}, nil
}

func watchHostProcess(parent context.Context, process hostProcess) (context.Context, func(), error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, nil, err
	}
	change := unix.Kevent_t{Ident: uint64(process.pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	if _, err := unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil); err != nil {
		unix.Close(queue)
		if errors.Is(err, unix.ESRCH) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if !process.current() {
		unix.Close(queue)
		return nil, nil, nil
	}

	ctx, cancel := context.WithCancel(parent)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer unix.Close(queue)
		defer cancel()
		var event [1]unix.Kevent_t
		for ctx.Err() == nil {
			timeout := unix.NsecToTimespec(int64(100 * time.Millisecond))
			count, err := unix.Kevent(queue, nil, event[:], &timeout)
			if count > 0 || (err != nil && !errors.Is(err, unix.EINTR)) {
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-exited }, nil
}
