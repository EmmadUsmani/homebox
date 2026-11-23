package hostinfo

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// Matches struct rusage_info_v0 in the macOS SDK's sys/resource.h.
type rusageInfoV0 struct {
	uuid               [16]byte
	userTime           uint64
	systemTime         uint64
	packageIdleWakeups uint64
	interruptWakeups   uint64
	pageins            uint64
	wiredSize          uint64
	residentSize       uint64
	physicalFootprint  uint64
	procStartAbstime   uint64
	procExitAbstime    uint64
}

func physicalFootprint(pid int) *uint64 {
	const procInfoCallPIDRusage = 9
	const rusageInfoV0Flavor = 0
	var info rusageInfoV0
	_, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDRusage, uintptr(pid),
		rusageInfoV0Flavor, 0, uintptr(unsafe.Pointer(&info)), 0)
	if errno != 0 {
		return nil
	}
	return &info.physicalFootprint
}
