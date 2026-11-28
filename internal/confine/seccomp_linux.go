package confine

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func installSocketFilter(daemon, qemu bool) error {
	const load = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
	const equal = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
	const greaterEqual = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
	const ret = unix.BPF_RET | unix.BPF_K
	allow := unix.SockFilter{Code: ret, K: unix.SECCOMP_RET_ALLOW}
	deny := unix.SockFilter{Code: ret, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)}
	program := []unix.SockFilter{
		{Code: load, K: 4}, // seccomp_data.arch
		{Code: equal, K: unix.AUDIT_ARCH_X86_64, Jt: 1},
		{Code: ret, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: load, K: 0},                         // seccomp_data.nr
		{Code: greaterEqual, K: 0x40000000, Jf: 1}, // x32 syscall ABI
		deny,
	}
	forbidden := []uint32{unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER}
	if !daemon {
		forbidden = append(forbidden, unix.SYS_SENDMMSG)
	}
	if qemu {
		// Lima passes the gateway as an already connected file descriptor.
		forbidden = append(forbidden, unix.SYS_CONNECT)
	}
	for _, syscall := range forbidden {
		program = append(program, unix.SockFilter{Code: equal, K: syscall, Jf: 1}, deny)
	}
	if !daemon {
		// TCP Fast Open can initiate an outbound connection from a send syscall.
		const anyBitSet = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K
		for _, call := range []struct {
			number      uint32
			flagsOffset uint32
		}{
			{unix.SYS_SENDTO, 40},  // seccomp_data.args[3]
			{unix.SYS_SENDMSG, 32}, // seccomp_data.args[2]
		} {
			program = append(program,
				unix.SockFilter{Code: equal, K: call.number, Jf: 4},
				unix.SockFilter{Code: load, K: call.flagsOffset},
				unix.SockFilter{Code: anyBitSet, K: unix.MSG_FASTOPEN, Jf: 1},
				deny,
				allow,
			)
		}
	}
	if qemu {
		// Unix datagrams can specify a destination in sendmsg without calling connect.
		// Check socketpair too so it cannot supply a datagram socket to the caller.
		program = append(program,
			unix.SockFilter{Code: equal, K: unix.SYS_SOCKET, Jt: 2},
			unix.SockFilter{Code: equal, K: unix.SYS_SOCKETPAIR, Jt: 1},
			allow,
			unix.SockFilter{Code: load, K: 16},
			unix.SockFilter{Code: equal, K: unix.AF_UNIX, Jt: 2},
			unix.SockFilter{Code: equal, K: unix.AF_INET, Jt: 1},
			deny,
		)
	} else {
		program = append(program,
			unix.SockFilter{Code: equal, K: unix.SYS_SOCKET, Jt: 1}, allow,
			unix.SockFilter{Code: load, K: 16}, // args[0] (domain)
			unix.SockFilter{Code: equal, K: unix.AF_UNIX, Jf: 1}, allow,
			unix.SockFilter{Code: equal, K: unix.AF_NETLINK, Jf: 4},
			unix.SockFilter{Code: load, K: 32}, // args[2] (protocol)
			unix.SockFilter{Code: equal, K: unix.NETLINK_ROUTE, Jt: 1},
			deny, allow,
			unix.SockFilter{Code: equal, K: unix.AF_INET, Jt: 1}, deny,
		)
	}
	program = append(program,
		unix.SockFilter{Code: load, K: 24}, // args[1] (type)
		unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: 0xf},
		unix.SockFilter{Code: equal, K: unix.SOCK_STREAM, Jf: 1}, allow,
	)
	if daemon {
		program = append(program, unix.SockFilter{Code: equal, K: unix.SOCK_DGRAM, Jf: 1}, allow)
	}
	program = append(program, deny)
	filter := unix.SockFprog{Len: uint16(len(program)), Filter: &program[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&filter)))
	if errno != 0 {
		return fmt.Errorf("apply socket seccomp filter: %w", errno)
	}
	return nil
}
