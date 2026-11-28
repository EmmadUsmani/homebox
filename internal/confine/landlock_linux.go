package confine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	readFile  = unix.LANDLOCK_ACCESS_FS_READ_FILE
	readTree  = readFile | unix.LANDLOCK_ACCESS_FS_READ_DIR
	execute   = readFile | unix.LANDLOCK_ACCESS_FS_EXECUTE
	writeFile = unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE
	writeTree = writeFile | unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_REFER
	bindTCP    = unix.LANDLOCK_ACCESS_NET_BIND_TCP
	connectTCP = unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
)

func install(r Rules, profile string, qemu bool) error {
	runtime.LockOSThread()
	if err := LinuxAvailable(); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	attr := [3]uint64{(1 << 16) - 1, bindTCP | connectTCP,
		unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET | unix.LANDLOCK_SCOPE_SIGNAL}
	if r.Daemon {
		// IPv4 connects are needed to enforce the gateway policy per flow.
		attr[1] = bindTCP
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("create Landlock ruleset: %w", errno)
	}
	defer unix.Close(int(fd))

	type fileRule struct {
		path   string
		rights uint64
	}
	files := []fileRule{{"/dev/null", readFile | writeFile},
		{"/usr/lib", readTree}, {"/lib64/ld-linux-x86-64.so.2", execute},
		{"/etc/ld.so.cache", readFile}}
	if qemu {
		files = append(files, fileRule{r.RuntimeDir, readTree | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE},
			fileRule{filepath.Join(r.RuntimeDir, "disk"), writeFile},
			fileRule{r.HostPIDFile, writeFile})
		for _, name := range qemuOutputFiles {
			files = append(files, fileRule{filepath.Join(r.RuntimeDir, name), writeFile})
		}
	} else {
		files = append(files, fileRule{r.RuntimeDir, readTree | writeTree}, fileRule{r.Launcher, execute})
	}
	if r.Daemon {
		files = append(files, fileRule{r.HostPIDFile, readFile})
	} else {
		files = append(files, fileRule{"/usr/bin/qemu-system-x86_64", execute},
			fileRule{"/usr/share/qemu", readTree}, fileRule{"/usr/share/OVMF", readTree},
			fileRule{"/usr/share/seabios", readTree}, fileRule{"/usr/share/ipxe-qemu", readTree},
			fileRule{"/dev/kvm", readFile | writeFile | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV},
			fileRule{"/dev/urandom", readFile}, fileRule{"/dev/random", readFile},
			fileRule{"/proc/cpuinfo", readFile}, fileRule{"/proc/meminfo", readFile},
			fileRule{"/sys/devices/system/cpu", readTree})
	}
	if !r.Daemon && !qemu {
		files = append(files, fileRule{profile, readFile}, fileRule{r.HostPIDFile, writeFile},
			fileRule{r.Executable, execute}, fileRule{r.LimaHome, unix.LANDLOCK_ACCESS_FS_READ_DIR},
			fileRule{filepath.Join(r.LimaHome, "_config"), readTree},
			fileRule{"/proc/sys/kernel/random/boot_id", readFile})
		for _, path := range []string{"/etc/localtime", "/etc/passwd", "/etc/group",
			"/etc/nsswitch.conf", "/etc/ssh/ssh_config", "/etc/ssh/ssh_config.d"} {
			files = append(files, fileRule{path, readTree})
		}
		for _, path := range []string{"/usr/bin/ssh", "/usr/bin/qemu-img", "/usr/bin/sh"} {
			files = append(files, fileRule{path, execute})
		}
	}
	for _, file := range files {
		if err := addFileRule(fd, file.path, file.rights); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Landlock path %s: %w", file.path, err)
		}
	}
	for _, bind := range r.BindTCP {
		if err := addPortRule(fd, bind.Port, bindTCP); err != nil {
			return err
		}
	}
	if !r.Daemon {
		// SLIRP reserves an ephemeral loopback source port for its Unix
		// forwards. It must be permitted in both inherited rulesets.
		if err := addPortRule(fd, 0, bindTCP); err != nil {
			return err
		}
	}
	if !qemu {
		for _, port := range r.ConnectTCP {
			if err := addPortRule(fd, port, connectTCP); err != nil {
				return err
			}
		}
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("apply Landlock ruleset: %w", errno)
	}
	return installSocketFilter(r.Daemon, qemu)
}

func addFileRule(fd uintptr, path string, rights uint64) error {
	f, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(f)
	var st unix.Stat_t
	if err := unix.Fstat(f, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		rights &= execute | writeFile | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	if rights == 0 {
		return nil
	}
	// landlock_path_beneath_attr is packed. The extra trailing bytes in
	// this Go struct are ignored by the kernel.
	attr := struct {
		Access uint64
		FD     int32
	}{rights, int32(f)}
	return addRule(fd, 1, unsafe.Pointer(&attr))
}

func addPortRule(fd uintptr, port int, rights uint64) error {
	if !validPort(port) && !(port == 0 && rights == bindTCP) {
		return fmt.Errorf("invalid Landlock TCP port %d", port)
	}
	attr := [2]uint64{rights, uint64(port)}
	return addRule(fd, 2, unsafe.Pointer(&attr))
}

func addRule(fd, kind uintptr, attr unsafe.Pointer) error {
	_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, kind, uintptr(attr), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
