package state

import "golang.org/x/sys/unix"

func exchangeImageDirs(staged, destination string) error {
	return unix.RenamexNp(staged, destination, unix.RENAME_SWAP)
}
