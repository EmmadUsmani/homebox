//go:build !darwin && !linux

package state

import "errors"

// WithLock reports that state locking is unavailable on this host.
func (s *Store) WithLock(func() error) error {
	return errors.New("state locking is unavailable on this host")
}
