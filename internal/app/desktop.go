package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/EmmadUsmani/homebox/internal/displayproxy"
	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/crypto/ssh"
)

// DesktopInstall adds the optional Ubuntu desktop packages to a running VM.
func (s *Service) DesktopInstall(ctx context.Context, name string) error {
	box, err := s.desktopVM(ctx, name)
	if err != nil {
		return err
	}

	client, err := s.desktopSSH(ctx, box)
	if err != nil {
		return err
	}
	defer client.Close()
	s.progress("Installing the desktop layer on %s", name)
	if err := displayproxy.Install(ctx, client, s.Profiles, box.Profile, s.Progress); err != nil {
		return fmt.Errorf("install desktop on %s: %w", name, err)
	}
	return nil
}

// Desktop starts the guest session and serves it until interrupted or SSH is lost.
// With noOpen, publish receives the one-time browser URL instead of opening it.
func (s *Service) Desktop(ctx context.Context, name string, noOpen bool, publish func(string) error) error {
	box, err := s.desktopVM(ctx, name)
	if err != nil {
		return err
	}
	client, err := s.desktopSSH(ctx, box)
	if err != nil {
		return err
	}
	defer client.Close()
	tunnel, closeTunnel := context.WithCancelCause(ctx)
	defer closeTunnel(nil)
	go func() {
		_ = client.Wait()
		closeTunnel(errors.New("desktop SSH connection lost"))
	}()
	go keepDesktopSSHAlive(tunnel, client)

	proxy, err := displayproxy.Open(tunnel, client, s.Progress)
	if errors.Is(err, displayproxy.ErrLayerMissing) {
		return fmt.Errorf("desktop layer missing on %s; run box desktop %s --install", name, name)
	}
	if err != nil {
		return err
	}
	defer func() {
		closeTunnel(nil)
		_ = proxy.Wait()
	}()
	if noOpen {
		err = publish(proxy.BootstrapURL)
	} else if s.OpenURL != nil {
		if s.OpenURL(tunnel, proxy.BootstrapURL) != nil {
			return errors.New("could not open the desktop browser; try --no-open")
		}
	} else {
		return errors.New("browser opener unavailable; use --no-open")
	}
	if err != nil {
		return err
	}
	if err := proxy.Wait(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return context.Cause(tunnel)
}

// DesktopProfile checks that a local VM can open a desktop tunnel.
func (s *Service) DesktopProfile(ctx context.Context, name string) (string, error) {
	box, err := s.desktopVM(ctx, name)
	return box.Profile, err
}

func (s *Service) desktopVM(ctx context.Context, name string) (state.Box, error) {
	box, err := s.load(name)
	if err != nil {
		return state.Box{}, err
	}
	instance, err := s.Driver.Get(ctx, name)
	if err != nil {
		return state.Box{}, err
	}
	if instance.Status != vm.StatusRunning {
		return state.Box{}, fmt.Errorf("box %q is %s; run box start %s", name, instance.Status, name)
	}
	for _, forward := range box.Forwards {
		if forward.GuestPort == displayproxy.GuestPort {
			return state.Box{}, fmt.Errorf("remove the published guest port %d before using desktop on %s", displayproxy.GuestPort, name)
		}
	}
	return box, nil
}

func (s *Service) desktopSSH(ctx context.Context, box state.Box) (*ssh.Client, error) {
	name := box.Name
	endpoint, err := s.Driver.SSH(ctx, name)
	if err != nil {
		return nil, err
	}
	endpoint.IdentityFile = box.SSHIdentityFile
	client, err := sshx.DialPinned(ctx, endpoint, box.SSHHostKeys)
	if errors.Is(err, sshx.ErrHostKeyChanged) {
		return nil, fmt.Errorf("SSH host key changed for box %q; if intentional, run box repin %s", name, name)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s over pinned SSH: %w", name, err)
	}
	return client, nil
}

func keepDesktopSSHAlive(ctx context.Context, client *ssh.Client) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timer := time.AfterFunc(10*time.Second, func() { _ = client.Close() })
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			timer.Stop()
			if err != nil {
				return
			}
		}
	}
}
