package confine

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSocketFilter(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("the Linux host filter targets x86-64")
	}
	// Install the actual BPF in a child so its rules cannot affect other tests.
	const profileEnv = "HOMEBOX_TEST_SOCKET_PROFILE"
	if profile := os.Getenv(profileEnv); profile != "" {
		runtime.LockOSThread()
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		daemon := profile == "vmnet"
		if err := installSocketFilter(daemon, profile == "qemu"); err != nil {
			t.Fatal(err)
		}
		if err := checkSocketFilter(daemon, profile == "qemu"); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, profile := range []string{"hostagent", "qemu", "vmnet"} {
		t.Run(profile, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSocketFilter$")
			cmd.Env = append(os.Environ(), profileEnv+"="+profile)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
