package lima

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/EmmadUsmani/homebox/internal/diskcopy"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// SaveDisk copies the standalone disk of a stopped VZ guest.
func (d *Driver) SaveDisk(ctx context.Context, name, path string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("saving images requires Lima with VZ on macOS")
	}
	record, err := d.find(ctx, name)
	if err != nil {
		return err
	}
	if record.VMType != "vz" {
		return fmt.Errorf("VM %q uses %q; saving images requires VZ", name, record.VMType)
	}
	instance := record.instance()
	if instance.Status != vm.StatusStopped {
		return fmt.Errorf("VM %q must be stopped; run box stop %s first", name, name)
	}
	disk := filepath.Join(instance.RuntimeDir, "disk")
	if _, err := os.Stat(disk); os.IsNotExist(err) {
		disk = filepath.Join(instance.RuntimeDir, "diffdisk")
	}
	return diskcopy.File(ctx, disk, path)
}

func installImage(ctx context.Context, instance vm.Instance, source string) error {
	destination := filepath.Join(instance.RuntimeDir, "disk")
	staged, err := os.MkdirTemp(instance.RuntimeDir, ".image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	disk := filepath.Join(staged, "disk")
	if err := diskcopy.File(ctx, source, disk); err != nil {
		return err
	}
	return os.Rename(disk, destination)
}

// Lima runs boot commands before sshd accepts connections and supplies a new
// cloud-init instance ID. The marker distinguishes a clone from later boots.
func imageBootScript(identity, user string) string {
	return fmt.Sprintf(`set -eu
marker=/etc/homebox-image-id
identity=%s
if [ "$(cat "$marker" 2>/dev/null || true)" = "$identity" ]; then
    exit 0
fi
# Lima ordinarily requests the host UID, which may belong to the source
# account on this disk. Allocate an unused UID for a new login name.
id '%s' >/dev/null 2>&1 || useradd -m -s /bin/bash '%s'
rm -f /etc/ssh/ssh_host_*_key /etc/ssh/ssh_host_*_key.pub
ssh-keygen -A
new_machine_id=$(tr -d '-' </proc/sys/kernel/random/uuid)
printf '%%s\n' "$new_machine_id" > /etc/machine-id
ln -sf /etc/machine-id /var/lib/dbus/machine-id
printf '%%s\n' "$identity" > "$marker"
`, identity, user, user)
}
