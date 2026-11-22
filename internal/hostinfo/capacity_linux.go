package hostinfo

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

// System reads the host's hardware capacity.
type System struct{}

func (System) Capacity() (cpus int, memoryBytes uint64, err error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, 0, fmt.Errorf("read sysinfo: %w", err)
	}
	return runtime.NumCPU(), uint64(info.Totalram) * uint64(info.Unit), nil
}
