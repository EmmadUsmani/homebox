package lima

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/diskcopy"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// SaveDisk copies the standalone disk of a stopped guest.
func (d *Driver) SaveDisk(ctx context.Context, name, path string) ([]byte, error) {
	record, err := d.find(ctx, name)
	if err != nil {
		return nil, err
	}
	if record.VMType != d.ImageBackend() || d.ImageBackend() == "" {
		return nil, fmt.Errorf("VM %q has unsupported image backend %q", name, record.VMType)
	}
	if record.Arch != limaArch(runtime.GOARCH) {
		return nil, fmt.Errorf("VM %q has unsupported guest architecture %q", name, record.Arch)
	}
	instance := record.instance()
	if instance.Status != vm.StatusStopped {
		return nil, fmt.Errorf("VM %q must be stopped; run box stop %s first", name, name)
	}
	disk := filepath.Join(instance.RuntimeDir, "disk")
	if record.VMType == "qemu" {
		if _, err := diskcopy.StandaloneQCOW2(disk); err != nil {
			return nil, err
		}
		return nil, diskcopy.File(ctx, disk, path)
	}
	if _, err := os.Stat(disk); os.IsNotExist(err) {
		disk = filepath.Join(instance.RuntimeDir, "diffdisk")
	}
	firmware, err := os.ReadFile(filepath.Join(instance.RuntimeDir, "vz-efi"))
	if err != nil {
		return nil, fmt.Errorf("read VZ EFI variables: %w", err)
	}
	return firmware, diskcopy.File(ctx, disk, path)
}

func limaArch(arch string) string {
	switch arch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return arch
	}
}

// Stage the disk before publishing the config. Lima's create command would
// prepare a second disk even though start can initialize the VM from this one.
func (d *Driver) createFromImage(ctx context.Context, spec vm.Spec, config []byte) error {
	if d.ImageBackend() == "vz" && len(spec.ImageFirmware) == 0 {
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
	if err := d.prepareImageSSHKey(ctx); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(d.options.Home, ".image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	disk := filepath.Join(staged, "disk")
	if err := diskcopy.File(ctx, spec.ImageDisk, disk); err != nil {
		return err
	}
	if d.ImageBackend() == "qemu" {
		if _, err := diskcopy.StandaloneQCOW2(disk); err != nil {
			return err
		}
	} else if err := os.WriteFile(filepath.Join(staged, "vz-efi"), spec.ImageFirmware, 0600); err != nil {
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

// Image staging bypasses limactl create, which initializes Lima's login key
// before the confined host agent starts.
func (d *Driver) prepareImageSSHKey(ctx context.Context) error {
	config := filepath.Join(d.options.Home, "_config")
	key := filepath.Join(config, "user")
	if _, err := os.Stat(key); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(config, 0700); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(config, ".key-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	stagedKey := filepath.Join(staged, "user")
	command := exec.CommandContext(ctx, "/usr/bin/ssh-keygen", "-t", "ed25519", "-q", "-N", "", "-C", "lima", "-f", stagedKey)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("initialize Lima SSH identity: %w: %s", err, output)
	}
	if err := os.Rename(stagedKey+".pub", key+".pub"); err != nil {
		return err
	}
	return os.Rename(stagedKey, key)
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
