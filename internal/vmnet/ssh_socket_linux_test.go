package vmnet

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestUnixSSHRequiresLaunchPeer(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "ssh")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	pidFile := filepath.Join(dir, "pid")
	writePID := func(pid int) {
		t.Helper()
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			t.Fatal(err)
		}
		record := fmt.Sprintf("%d %d %d\n", pid, stat.Dev, stat.Ino)
		if err := os.WriteFile(pidFile, []byte(record), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePID(os.Getpid())
	if !OwnsUnixSSHTarget(t.Context(), pidFile, socket) {
		t.Fatal("refused Unix listener owned by the launch process")
	}

	other := exec.CommandContext(t.Context(), "/bin/sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer other.Wait()
	defer other.Process.Kill()
	writePID(other.Process.Pid)
	if OwnsUnixSSHTarget(t.Context(), pidFile, socket) {
		t.Fatal("accepted Unix listener owned by a different process")
	}
}
