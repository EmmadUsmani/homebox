package state

import "fmt"

// MinimumFreeBytes is the available-space floor for VM starts and disk writes.
const MinimumFreeBytes = 10 << 30

func (s *Store) RequireFreeSpace() error {
	free, err := s.FreeBytes()
	if err != nil {
		return err
	}
	return checkFreeSpace(free)
}

func checkFreeSpace(free uint64) error {
	if free < MinimumFreeBytes {
		return fmt.Errorf("host state volume has %.1f GiB available; free space until at least %d GiB is available", float64(free)/(1<<30), MinimumFreeBytes>>30)
	}
	return nil
}
