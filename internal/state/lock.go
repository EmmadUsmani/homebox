//go:build darwin || linux

package state

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// WithLock runs an operation under the state directory's cross-process lock.
func (s *Store) WithLock(operation func() error) error {
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(s.Root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		return operation()
	}
}
