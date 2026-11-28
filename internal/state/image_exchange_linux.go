package state

import "golang.org/x/sys/unix"

func exchangeImageDirs(staged, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, staged, unix.AT_FDCWD, destination, unix.RENAME_EXCHANGE)
}
