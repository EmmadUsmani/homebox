//go:build darwin || linux

package state

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// FreeBytes reports bytes available to an unprivileged process on the state volume.
func (s *Store) FreeBytes() (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(s.Root, &stat); err != nil {
		return 0, fmt.Errorf("check host state volume: %w", err)
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
