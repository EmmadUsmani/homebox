package diskcopy

import "golang.org/x/sys/unix"

func clone(source, destination string) bool {
	return unix.Clonefile(source, destination, 0) == nil
}
