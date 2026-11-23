package hostinfo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const virtualizationProcess = "com.apple.Virtualization.VirtualMachine"

type diskIdentity struct {
	device uint64
	inode  uint64
}

func virtualizationDisk(runtimeDir string) (string, diskIdentity, bool) {
	if runtimeDir == "" {
		return "", diskIdentity{}, false
	}
	dir, err := filepath.EvalSymlinks(runtimeDir)
	if err != nil {
		return "", diskIdentity{}, false
	}
	path := filepath.Join(dir, "disk")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", diskIdentity{}, false
	}
	stat := info.Sys().(*syscall.Stat_t)
	return lsofPath(path), diskIdentity{device: uint64(uint32(stat.Dev)), inode: stat.Ino}, true
}

// lsof escapes control bytes and backslashes even with NUL-separated output.
func lsofPath(path string) string {
	var encoded strings.Builder
	for _, char := range []byte(path) {
		switch char {
		case '\\':
			encoded.WriteString(`\\`)
		case '\n':
			encoded.WriteString(`\n`)
		case '\t':
			encoded.WriteString(`\t`)
		case '\r':
			encoded.WriteString(`\r`)
		case '\b':
			encoded.WriteString(`\b`)
		case '\f':
			encoded.WriteString(`\f`)
		default:
			switch {
			case char < ' ':
				encoded.WriteByte('^')
				encoded.WriteByte(char + '@')
			case char == 127:
				encoded.WriteString("^?")
			case char > 127:
				fmt.Fprintf(&encoded, `\x%02x`, char)
			default:
				encoded.WriteByte(char)
			}
		}
	}
	return encoded.String()
}

func virtualizationPIDs(output []byte, disks map[string]diskIdentity) map[string]int {
	byPath := make(map[string]int)
	byPID := make(map[int]string)
	var pid int
	var command, name string
	var descriptor bool
	var identity diskIdentity
	markDisk := func() {
		if disk, ok := disks[name]; ok && disk == identity && descriptor && command == virtualizationProcess && pid > 1 {
			if prior, exists := byPath[name]; exists && prior != pid {
				byPath[name] = 0
			} else {
				byPath[name] = pid
			}
			if prior, exists := byPID[pid]; exists && prior != name {
				byPID[pid] = ""
			} else {
				byPID[pid] = name
			}
		}
		name, identity, descriptor = "", diskIdentity{}, false
	}
	for field := range bytes.SplitSeq(output, []byte{0}) {
		field = bytes.TrimPrefix(field, []byte{'\n'})
		if len(field) < 2 {
			continue
		}
		value := string(field[1:])
		switch field[0] {
		case 'p':
			markDisk()
			pid, _ = strconv.Atoi(value)
			command = ""
		case 'c':
			command = value
		case 'f':
			markDisk()
			_, err := strconv.ParseUint(value, 10, 32)
			descriptor = err == nil
		case 'D':
			if strings.HasPrefix(value, "0x") {
				identity.device, _ = strconv.ParseUint(value[2:], 16, 64)
			}
		case 'i':
			identity.inode, _ = strconv.ParseUint(value, 10, 64)
		case 'n':
			name = value
		}
	}
	markDisk()
	for path, pid := range byPath {
		if byPID[pid] != path {
			delete(byPath, path)
		}
	}
	return byPath
}
