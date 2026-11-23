package lima

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Resize atomically updates the Lima config. Lima grows the boot disk on start.
func (d *Driver) Resize(ctx context.Context, name string, size vm.Size) error {
	instance, err := d.Get(ctx, name)
	if err != nil {
		return err
	}
	if instance.Status != vm.StatusStopped {
		return fmt.Errorf("VM %q must be stopped; run box stop %s first", name, name)
	}
	if size.DiskGiB < instance.Size.DiskGiB {
		return fmt.Errorf("cannot shrink the disk from %d to %d GiB", instance.Size.DiskGiB, size.DiskGiB)
	}
	path := filepath.Join(instance.RuntimeDir, "lima.yaml")
	config, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Lima 2.3 writes edits in place. Stage beside the original so relative config
	// paths retain their meaning and an interrupted edit leaves the original intact.
	staged, err := os.CreateTemp(instance.RuntimeDir, ".resize-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	_, writeErr := staged.Write(config)
	if err := errors.Join(writeErr, staged.Close()); err != nil {
		return err
	}
	changes := fmt.Sprintf(".cpus = %d | .memory = \"%dGiB\" | .disk = \"%dGiB\"", size.CPUs, size.MemoryGiB, size.DiskGiB)
	if _, err := d.run(ctx, nil, "edit", "--tty=false", "--set", changes, staged.Name()); err != nil {
		return err
	}
	return os.Rename(staged.Name(), path)
}
