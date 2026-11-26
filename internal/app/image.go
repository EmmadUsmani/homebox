package app

import (
	"context"
	"fmt"
	"runtime"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

func (s *Service) SaveImage(ctx context.Context, name, image, version string) error {
	driver, ok := s.Driver.(interface {
		SaveDisk(context.Context, string, string) ([]byte, error)
	})
	if !ok {
		return fmt.Errorf("driver %q does not support saving images", s.Driver.Name())
	}
	return s.Store.WithLock(func() error {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil {
			return err
		}
		if instance.Status != vm.StatusStopped {
			return fmt.Errorf("VM %q must be stopped; run box stop %s first", name, name)
		}
		return s.Store.SaveImage(ctx, box, image, version, func(disk string) ([]byte, error) {
			return driver.SaveDisk(ctx, name, disk)
		})
	})
}

func (s *Service) ListImages() ([]state.Image, error) {
	var images []state.Image
	err := s.Store.WithLock(func() error {
		var err error
		images, err = s.Store.ListImages()
		return err
	})
	return images, err
}

func (s *Service) RemoveImage(name string) error {
	return s.Store.WithLock(func() error { return s.Store.RemoveImage(name) })
}

func (s *Service) ExportImage(ctx context.Context, name, path string) error {
	return s.Store.ExportImage(ctx, name, path)
}

func (s *Service) ImportImage(ctx context.Context, path string) (string, error) {
	var name string
	err := s.Store.WithLock(func() error {
		var err error
		name, err = s.Store.ImportImage(ctx, path)
		return err
	})
	return name, err
}

func (s *Service) resolveImageOptions(options *NewOptions) error {
	image, _, err := s.Store.LoadImage(options.Image)
	if err != nil {
		return fmt.Errorf("load image %q: %w", options.Image, err)
	}
	if image.Driver != s.Driver.Name() || image.Arch != runtime.GOARCH {
		return fmt.Errorf("image %q needs driver %q and architecture %q", image.Name, image.Driver, image.Arch)
	}
	if options.Profile != "" && options.Profile != image.Profile {
		return fmt.Errorf("image %q uses profile %q", image.Name, image.Profile)
	}
	if options.DiskGiB != 0 && options.DiskGiB < image.DiskGiB {
		return fmt.Errorf("image %q requires a disk of at least %d GiB", image.Name, image.DiskGiB)
	}
	options.Profile = image.Profile
	options.DiskGiB = sizeOrDefault(options.DiskGiB, image.DiskGiB)
	return nil
}
