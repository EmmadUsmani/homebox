package hostinfo

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// System reads the host's hardware capacity.
type System struct{}

func (System) Capacity() (cpus int, memoryBytes uint64, err error) {
	cpuCount, err := unix.SysctlUint32("hw.ncpu")
	if err != nil {
		return 0, 0, fmt.Errorf("read hw.ncpu: %w", err)
	}
	memory, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, 0, fmt.Errorf("read hw.memsize: %w", err)
	}
	return int(cpuCount), memory, nil
}
