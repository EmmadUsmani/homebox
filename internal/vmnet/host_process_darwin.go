package vmnet

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func watchHostProcess(parent context.Context, pid int, written time.Time) (context.Context, func(), error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, nil, err
	}
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	if _, err := unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil); err != nil {
		unix.Close(queue)
		if errors.Is(err, unix.ESRCH) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info.Proc.P_pid != int32(pid) ||
		time.Unix(info.Proc.P_starttime.Unix()).After(written) {
		unix.Close(queue)
		return nil, nil, nil
	}

	ctx, cancel := context.WithCancel(parent)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer unix.Close(queue)
		defer cancel()
		for ctx.Err() == nil {
			timeout := unix.NsecToTimespec(int64(100 * time.Millisecond))
			event := make([]unix.Kevent_t, 1)
			count, err := unix.Kevent(queue, nil, event, &timeout)
			if count > 0 || (err != nil && !errors.Is(err, unix.EINTR)) {
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-exited }, nil
}
