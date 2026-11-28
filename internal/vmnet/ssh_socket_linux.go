package vmnet

import (
	"context"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func (p hostProcess) unixDial(path string) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		if !p.current() {
			return nil, fmt.Errorf("QEMU process has exited")
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
		if err != nil {
			return nil, err
		}
		raw, err := conn.(*net.UnixConn).SyscallConn()
		if err == nil {
			var credentials *unix.Ucred
			var socketErr error
			err = raw.Control(func(fd uintptr) {
				credentials, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
				if socketErr == nil && p.inode != 0 {
					peer, err := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PEERPIDFD)
					if err != nil {
						socketErr = err
						return
					}
					defer unix.Close(peer)
					if !p.matchesDescriptor(peer) {
						socketErr = fmt.Errorf("QEMU SSH peer pidfd does not match the launch identity")
					}
				}
			})
			if err == nil && socketErr == nil && int(credentials.Pid) == p.pid &&
				credentials.Uid == uint32(os.Geteuid()) && p.current() {
				return conn, nil
			}
		}
		conn.Close()
		return nil, fmt.Errorf("unverified QEMU SSH socket peer")
	}
}

// OwnsUnixSSHTarget verifies the Unix peer against the live QEMU PID and start time.
func OwnsUnixSSHTarget(ctx context.Context, pidFile, path string) bool {
	process, err := resolveHostProcess(pidFile)
	if err != nil || process.pid == 0 {
		return false
	}
	conn, err := process.unixDial(path)(ctx)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
