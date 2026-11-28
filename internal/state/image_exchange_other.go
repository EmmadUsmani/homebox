//go:build !darwin && !linux

package state

import "errors"

func exchangeImageDirs(staged, destination string) error {
	return errors.ErrUnsupported
}
