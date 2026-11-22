//go:build !darwin && !linux

package vmnet

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("VM network daemon is not supported yet on this platform")

// Run reports that the daemon is unavailable on this platform.
func Run(context.Context, Config) error { return errUnsupported }

// Spawn reports that detached processes are unavailable on this platform.
func Spawn(string, []string, Config) (int, error) { return 0, errUnsupported }

// Stop reports that the daemon is unavailable on this platform.
func Stop(string) error { return errUnsupported }

// Running reports that no daemon is running on this platform.
func Running(string) (int, bool) { return 0, false }
