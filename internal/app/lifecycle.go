package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
		if instance.GuestOS == vm.DarwinGuest && !box.Provisioned {
			return fmt.Errorf("macOS guest %q requires box provision before it can start", name)
		}
		if err := s.start(ctx, &box, instance); err != nil {
			return err
		}
		// Keys added with `box host init` since the last start reach the VM now.
		endpoint, err := s.pinSSH(ctx, &box)
		if err != nil {
			return err
		}
		return s.authorizeLaptopKeys(ctx, name, endpoint)
	})
}

// authorizeLaptopKeys puts the host's laptop keys into the guest; it is idempotent.
func (s *Service) authorizeLaptopKeys(ctx context.Context, name string, endpoint vm.Endpoint) error {
	host, err := s.Store.LoadHost()
	if err != nil || len(host.AuthorizedKeys) == 0 {
		return err
	}
	s.progress("Authorizing laptop keys for %s", name)
	return s.GuestSSH.AuthorizeKeys(ctx, endpoint, host.AuthorizedKeys)
}

func (s *Service) start(ctx context.Context, box *state.Box, instance vm.Instance) error {
	if instance.Status == vm.StatusRunning {
		return s.restartDeadIsolation(ctx, box, instance)
	}
	if err := s.applyLANSetting(ctx, box); err != nil {
		return err
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
	box.ProxyURL, box.NoProxy, box.DNS = session.ProxyURL, session.NoProxy, session.DNS
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
		if _, err := s.loadRecord(name); err != nil {
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
		if _, err := s.loadRecord(name); err != nil {
			return err
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil && !errors.Is(err, vm.ErrNotFound) {
			return err
		}
		if err == nil && instance.Status != vm.StatusStopped {
			s.progress("Stopping %s", name)
			// The VM is being deleted anyway, so a broken VM that won't shut
			// down cleanly must not block its removal.
			if err := s.Driver.Stop(ctx, name); err != nil {
				s.progress("Graceful stop failed (%v); deleting %s anyway", err, name)
			}
		}
		s.progress("Removing %s", name)
		if err := s.Driver.Delete(ctx, name); err != nil {
			return err
		}
		if err := s.releaseIsolation(ctx, name); err != nil {
			return err
		}
		if err := s.Store.Delete(name); err != nil {
			return err
		}
		return s.refreshLocalSSH()
	})
}

func (s *Service) releaseIsolation(ctx context.Context, name string) error {
	if s.Isolation == nil {
		return errors.New("host isolation is not configured")
	}
	s.progress("Releasing isolation for %s", name)
	return s.Isolation.Down(context.WithoutCancel(ctx), name)
}

// applyLANSetting rebinds a stopped box's published ports when the host's LAN
// setting changed since they were last set, so `box host init --lan` reaches
// existing boxes at their next start. The SSH relay follows the setting on its
// own; see internal/isolation.
func (s *Service) applyLANSetting(ctx context.Context, box *state.Box) error {
	host, err := s.Store.LoadHost()
	if err != nil {
		return err
	}
	want := state.HostIP(host.LAN)
	forwards := slices.Clone(box.Forwards)
	changed := false
	for i := range forwards {
		if forwards[i].HostIP != want {
			forwards[i].HostIP, changed = want, true
		}
	}
	if !changed {
		return nil
	}
	s.progress("Publishing %s's ports on %s", box.Name, want)
	if err := s.Driver.SetForwards(ctx, box.Name, forwards); err != nil {
		return err
	}
	box.Forwards = forwards
	return s.Store.Save(*box)
}

// restartDeadIsolation brings back the host-side services of a running VM
// whose network daemon died, e.g. after it was killed. The VM keeps its
// confinement; it only lost its way out until now.
func (s *Service) restartDeadIsolation(ctx context.Context, box *state.Box, instance vm.Instance) error {
	if s.Isolation.Running(box.Name) {
		return nil
	}
	s.progress("%s is running but its network daemon isn't; restarting it", box.Name)
	if _, err := s.Isolation.Up(ctx, *box, instance); err != nil {
		return fmt.Errorf("restart isolation: %w", err)
	}
	return nil
}
