package vmnet

import (
	"encoding/binary"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/unix"
)

func tcpListenerID(pid int, target netip.AddrPort) uint64 {
	const (
		procInfoCallPIDInfo   = 2
		procInfoCallPIDFDInfo = 3
		procPIDListFDs        = 1
		procPIDFDSocketInfo   = 3
		procFDTypeSocket      = 2
		socketInfoTCP         = 2
		tcpListen             = 1

		fdInfoSize             = 8
		socketInfoSize         = 792
		socketFamilyOffset     = 184
		socketOptionsOffset    = 188
		socketKindOffset       = 256
		socketPortOffset       = 268
		socketGenerationOffset = 272
		socketAddressOffset    = 324
		tcpStateOffset         = 344
	)
	// proc_fdinfo records contain a native-endian descriptor and type. Grow
	// the buffer if the process opened more descriptors since the size query.
	size, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDListFDs, 0, 0, 0)
	if errno != 0 || size == 0 {
		return 0
	}
	for size += fdInfoSize; size <= 8<<20; size *= 2 {
		descriptors := make([]byte, size)
		count, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDListFDs, 0,
			uintptr(unsafe.Pointer(&descriptors[0])), size)
		if errno != 0 || count > size || count%fdInfoSize != 0 {
			return 0
		}
		if count == size {
			continue
		}
		for offset := uintptr(0); offset < count; offset += fdInfoSize {
			if binary.NativeEndian.Uint32(descriptors[offset+4:]) != procFDTypeSocket {
				continue
			}
			fd := binary.NativeEndian.Uint32(descriptors[offset:])
			// Size and field offsets of socket_fdinfo on 64-bit macOS,
			// from the SDK's sys/proc_info.h.
			var info [socketInfoSize]byte
			n, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDFDInfo, uintptr(pid), procPIDFDSocketInfo,
				uintptr(fd), uintptr(unsafe.Pointer(&info[0])), uintptr(len(info)))
			if errno == 0 && n == uintptr(len(info)) &&
				binary.NativeEndian.Uint32(info[socketFamilyOffset:]) == unix.AF_INET &&
				binary.NativeEndian.Uint32(info[socketKindOffset:]) == socketInfoTCP &&
				binary.BigEndian.Uint16(info[socketPortOffset:]) == target.Port() &&
				[4]byte(info[socketAddressOffset:socketAddressOffset+4]) == target.Addr().As4() &&
				binary.NativeEndian.Uint32(info[tcpStateOffset:]) == tcpListen &&
				binary.NativeEndian.Uint16(info[socketOptionsOffset:])&unix.SO_REUSEPORT == 0 {
				return binary.NativeEndian.Uint64(info[socketGenerationOffset:])
			}
		}
		return 0
	}
	return 0
}
