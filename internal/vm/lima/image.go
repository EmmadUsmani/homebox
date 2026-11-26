package lima

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/diskcopy"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// SaveDisk copies the standalone disk of a stopped VZ guest.
func (d *Driver) SaveDisk(ctx context.Context, name, path string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("saving images requires Lima with VZ on macOS")
	}
	record, err := d.find(ctx, name)
	if err != nil {
		return nil, err
	}
	if record.VMType != "vz" {
		return nil, fmt.Errorf("VM %q uses %q; saving images requires VZ", name, record.VMType)
	}
	instance := record.instance()
	if instance.Status != vm.StatusStopped {
		return nil, fmt.Errorf("VM %q must be stopped; run box stop %s first", name, name)
	}
	disk := filepath.Join(instance.RuntimeDir, "disk")
	if _, err := os.Stat(disk); os.IsNotExist(err) {
		disk = filepath.Join(instance.RuntimeDir, "diffdisk")
	}
	firmware, err := os.ReadFile(filepath.Join(instance.RuntimeDir, "vz-efi"))
	if err != nil {
		return nil, fmt.Errorf("read VZ EFI variables: %w", err)
	}
	return firmware, diskcopy.File(ctx, disk, path)
}

// Stage the disk before publishing the config. Lima's create command would
// prepare a second disk even though start can initialize the VM from this one.
func (d *Driver) createFromImage(ctx context.Context, spec vm.Spec, config []byte) error {
	if len(spec.ImageFirmware) == 0 {
		return fmt.Errorf("saved image is missing its VZ EFI variables")
	}
	result, err := d.run(ctx, nil, "--version")
	if err != nil {
		return err
	}
	version, ok := strings.CutPrefix(strings.TrimSpace(string(result.Stdout)), "limactl version ")
	if !ok || version == "" {
		return fmt.Errorf("could not determine the Lima version")
	}
	if err := os.MkdirAll(d.options.Home, 0700); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(d.options.Home, ".image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err := diskcopy.File(ctx, spec.ImageDisk, filepath.Join(staged, "disk")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, "vz-efi"), spec.ImageFirmware, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, "lima.yaml"), config, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, "lima-version"), []byte(version), 0444); err != nil {
		return err
	}
	destination := filepath.Join(d.options.Home, spec.Name)
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("Lima instance directory %q already exists", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(staged, destination)
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
