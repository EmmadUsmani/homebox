//go:build !darwin && !linux

package confine

import (
	"fmt"
	"runtime"
)

// Prepare reports that host confinement is unavailable on this platform.
func Prepare(Rules, string) ([]string, error) {
	return nil, fmt.Errorf("host confinement is not supported yet on %s", runtime.GOOS)
}
