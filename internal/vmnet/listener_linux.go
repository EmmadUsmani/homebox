package vmnet

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func tcpListenerID(pid int, target netip.AddrPort) uint64 {
	table, err := os.Open("/proc/net/tcp")
	if err != nil {
		return 0
	}
	defer table.Close()
	inode := tcpListenerInode(table, target)
	if inode == 0 {
		return 0
	}
	socket := fmt.Sprintf("socket:[%d]", inode)
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	descriptors, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, descriptor := range descriptors {
		link, err := os.Readlink(filepath.Join(dir, descriptor.Name()))
		if err == nil && link == socket {
			return inode
		}
	}
	return 0
}

func tcpListenerInode(table io.Reader, target netip.AddrPort) uint64 {
	address := target.Addr().As4()
	local := fmt.Sprintf("%08X:%04X", binary.NativeEndian.Uint32(address[:]), target.Port())
	var inode uint64
	scanner := bufio.NewScanner(table)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 10 && fields[1] == local && fields[3] == "0A" {
			if inode != 0 {
				return 0
			}
			parsed, err := strconv.ParseUint(fields[9], 10, 64)
			if err != nil || parsed == 0 {
				return 0
			}
			inode = parsed
		}
	}
	if scanner.Err() != nil {
		return 0
	}
	return inode
}
