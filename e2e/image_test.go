package e2e

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/state"
)

func checkImage(t *testing.T, name, binary string, store *state.Store, box func(time.Duration, ...string) string, run func(time.Duration, string, ...string) (string, error), cleanupVM func(string)) {
	t.Helper()
	const clone = "e2e-image"
	const image = "e2e-saved"
	identity := func(name string) []string {
		t.Helper()
		fields := strings.Fields(box(time.Minute, "ssh", name, "--", "hostname && cat /etc/machine-id && cloud-init query instance_id"))
		if len(fields) != 3 {
			t.Fatalf("expected hostname, machine ID and cloud-init instance ID, got %v", fields)
		}
		return fields
	}
	original := identity(name)
	source, err := store.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := run(time.Minute, binary, "image", "save", name, image); err == nil || !strings.Contains(out, "must be stopped") {
		t.Fatalf("expected refusal to save a running VM: %v\n%s", err, out)
	}
	box(time.Minute, "ssh", name, "--", "printf image-test > ~/saved-marker")
	box(2*time.Minute, "stop", name)
	box(4*time.Minute, "image", "save", name, image)
	if out := box(time.Minute, "image", "ls"); !strings.Contains(out, image) {
		t.Fatalf("saved image absent from list: %s", out)
	}
	saved, _, err := store.LoadImage(image)
	if err != nil {
		t.Fatal(err)
	}
	copyStore := &state.Store{Root: filepath.Join(filepath.Dir(store.Root), "image-copy")}
	if out, err := run(4*time.Minute, "env", "HOMEBOX_HOME="+copyStore.Root, binary,
		"image", "copy", image, "--from-home", store.Root); err != nil {
		t.Fatalf("copy saved image: %v\n%s", err, out)
	}
	copied, _, err := copyStore.LoadImage(image)
	if err != nil || copied.SHA256 != saved.SHA256 || copied.Backend != saved.Backend ||
		copied.DiskFormat != saved.DiskFormat || copied.Arch != saved.Arch {
		t.Fatalf("copied image metadata changed: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "image.hbimg.gz")
	box(6*time.Minute, "image", "export", image, archive)
	box(time.Minute, "image", "rm", image)
	box(6*time.Minute, "image", "import", archive)
	if out, err := run(time.Minute, binary, "new", clone, "--image", image, "--disk", "23"); err == nil || !strings.Contains(out, "at least 24 GiB") {
		t.Fatalf("expected undersized disk to be refused: %v\n%s", err, out)
	}
	cleanupVM(clone)
	box(5*time.Minute, "new", clone, "--image", image, "--cpus", "2", "--memory", "4")
	changed := identity(clone)
	for index := range changed {
		if changed[index] == original[index] {
			t.Fatalf("cloned identity field %d did not change", index)
		}
	}
	target, err := store.Load(clone)
	if err != nil {
		t.Fatal(err)
	}
	if !target.Provisioned || target.Index == source.Index || target.SSHPort == source.SSHPort ||
		target.User != source.User || target.DiskGiB != source.DiskGiB {
		t.Fatal("clone did not inherit the disk size and receive fresh VM settings")
	}
	for _, key := range target.SSHHostKeys {
		if slices.Contains(source.SSHHostKeys, key) {
			t.Fatal("clone reused a source SSH host key")
		}
	}
	// SSH uses the generated local aliases and pins as a laptop would.
	if out, err := run(time.Minute, "ssh", "-F", store.SSHConfigPath(), clone, "cat ~/saved-marker"); err != nil || strings.TrimSpace(out) != "image-test" {
		t.Fatalf("pinned SSH did not read copied data: %v\n%s", err, out)
	}
	box(time.Minute, "ssh", clone, "--", `sudo -n systemd-run --wait --collect --unit=homebox-image-journal /bin/echo fresh-identity &&
		sudo -n journalctl --sync &&
		journalctl -b -u homebox-image-journal -o cat --no-pager | grep -Fx fresh-identity`)
	box(2*time.Minute, "doctor", clone)
	box(2*time.Minute, "stop", clone)
	box(3*time.Minute, "start", clone)
	restarted := identity(clone)
	if !slices.Equal(restarted[:2], changed[:2]) {
		t.Fatal("the clone's hostname or machine ID changed on restart")
	}
	box(2*time.Minute, "rm", "-f", clone)
}
