package state

import "testing"

func TestFreeSpaceFloor(t *testing.T) {
	for _, free := range []uint64{0, MinimumFreeBytes - 1, MinimumFreeBytes, MinimumFreeBytes + 1} {
		if err := checkFreeSpace(free); (err != nil) != (free < MinimumFreeBytes) {
			t.Errorf("checkFreeSpace(%d) = %v", free, err)
		}
	}
}
