package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Start brings up confinement and starts a stored VM.
func (s *Service) Start(ctx context.Context, name string) error {
	return s.Store.WithLock(func() error {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil {
			return err
		}
		return s.start(ctx, &box, instance)
	})
}

func (s *Service) start(ctx context.Context, box *state.Box, instance vm.Instance) error {
	if instance.Status == vm.StatusRunning {
		return nil
	}
	s.progress("Preparing isolation for %s", box.Name)
	if s.Isolation == nil {
		return errors.New("host isolation is not configured")
	}
	session, err := s.Isolation.Up(ctx, *box, instance)
	if err != nil {
		return fmt.Errorf("prepare isolation: %w", err)
	}
	cleanup := func(err error) error {
		return errors.Join(err, s.releaseIsolation(ctx, box.Name))
	}
	box.ProxyURL, box.NoProxy = session.ProxyURL, session.NoProxy
	if err := s.Store.Save(*box); err != nil {
		return cleanup(err)
	}
	s.progress("Starting %s", box.Name)
	if err := s.Driver.Start(ctx, box.Name, session.Start); err != nil {
		return cleanup(fmt.Errorf("start VM: %w", err))
	}
	return nil
}

// Stop stops the guest before tearing down isolation.
func (s *Service) Stop(ctx context.Context, name string) error {
	return s.Store.WithLock(func() error {
		if _, err := s.load(name); err != nil {
			return err
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil && !errors.Is(err, vm.ErrNotFound) {
			return err
		}
		if err == nil && instance.Status != vm.StatusStopped {
			s.progress("Stopping %s", name)
			if err := s.Driver.Stop(ctx, name); err != nil {
				return err
			}
		}
		return s.releaseIsolation(ctx, name)
	})
}

// Remove stops and deletes the VM, releases isolation and deletes its record.
func (s *Service) Remove(ctx context.Context, name string) error {
	return s.Store.WithLock(func() error {
		if _, err := s.load(name); err != nil {
			return err
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil && !errors.Is(err, vm.ErrNotFound) {
			return err
		}
		if err == nil && instance.Status != vm.StatusStopped {
			s.progress("Stopping %s", name)
			if err := s.Driver.Stop(ctx, name); err != nil {
				return err
			}
		}
		s.progress("Removing %s", name)
		if err := s.Driver.Delete(ctx, name); err != nil {
			return err
		}
		if err := s.releaseIsolation(ctx, name); err != nil {
			return err
		}
		return s.Store.Delete(name)
	})
}

func (s *Service) releaseIsolation(ctx context.Context, name string) error {
	if s.Isolation == nil {
		return errors.New("host isolation is not configured")
	}
	s.progress("Releasing isolation for %s", name)
	return s.Isolation.Down(context.WithoutCancel(ctx), name)
}
