//go:build !darwin && !linux

package state

import "errors"

func (*Store) FreeBytes() (uint64, error) {
	return 0, errors.New("host state volume free space unavailable on this platform")
}
