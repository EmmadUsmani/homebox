package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Resize reports the current size or updates a stopped VM. Zero fields keep
// their existing values.
func (s *Service) Resize(ctx context.Context, name string, changes vm.Size) (vm.Size, error) {
	var size vm.Size
	err := s.Store.WithLock(func() error {
		box, err := s.loadRecord(name)
		if err != nil {
			return err
		}
		size = vm.Size{CPUs: box.CPUs, MemoryGiB: box.MemoryGiB, DiskGiB: box.DiskGiB}
		if changes == (vm.Size{}) && box.PendingResize == nil {
			return nil
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil {
			return err
		}
		if box.PendingResize != nil {
			if err := s.finishResize(&box, size, instance.Size); err != nil {
				return err
			}
			size = instance.Size
		}
		if changes == (vm.Size{}) {
			return nil
		}
		if instance.Status != vm.StatusStopped {
			return fmt.Errorf("VM %q must be stopped; run box stop %s first", name, name)
		}
		if changes.CPUs < 0 || changes.MemoryGiB < 0 || changes.DiskGiB < 0 {
			return errors.New("CPU, memory and disk sizes must be positive")
		}
		if instance.Size != size {
			return fmt.Errorf("box %q size differs from the driver configuration", name)
		}
		if changes.DiskGiB > 0 && changes.DiskGiB < size.DiskGiB {
			return fmt.Errorf("cannot shrink the disk from %d to %d GiB", size.DiskGiB, changes.DiskGiB)
		}
		if changes.CPUs > 0 || changes.MemoryGiB > 0 {
			if _, err := s.checkHostSize(changes.CPUs, changes.MemoryGiB); err != nil {
				return err
			}
		}
		target := vm.Size{
			CPUs:      sizeOrDefault(changes.CPUs, size.CPUs),
			MemoryGiB: sizeOrDefault(changes.MemoryGiB, size.MemoryGiB),
			DiskGiB:   sizeOrDefault(changes.DiskGiB, size.DiskGiB),
		}
		if target == size {
			return nil
		}
		box.PendingResize = &target
		if err := s.Store.Save(box); err != nil {
			return err
		}
		if err := s.Driver.Resize(ctx, name, target); err != nil {
			// A failed driver update leaves its configuration intact.
			return errors.Join(err, s.finishResize(&box, size, size))
		}
		if err := s.finishResize(&box, size, target); err != nil {
			return err
		}
		size = target
		return nil
	})
	return size, err
}

// The journal and old size share one record. Either outcome can be finalized
// after an interruption without guessing whether the driver changed.
func (s *Service) finishResize(box *state.Box, previous, configured vm.Size) error {
	if configured != previous && configured != *box.PendingResize {
		return fmt.Errorf("cannot recover resize for %q: driver size matches neither the previous nor requested size", box.Name)
	}
	box.CPUs, box.MemoryGiB, box.DiskGiB = configured.CPUs, configured.MemoryGiB, configured.DiskGiB
	box.PendingResize = nil
	if err := s.Store.Save(*box); err != nil {
		return errors.Join(err, pendingResizeError(box.Name))
	}
	return nil
}

func pendingResizeError(name string) error {
	return fmt.Errorf("box %q has an unfinished resize; run box resize %s to recover it", name, name)
}
