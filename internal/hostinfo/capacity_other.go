//go:build !darwin && !linux

package hostinfo

import "runtime"

// System provides CPU capacity on platforms without a memory probe.
type System struct{}

func (System) Capacity() (cpus int, memoryBytes uint64, err error) {
	return runtime.NumCPU(), 0, nil
}
