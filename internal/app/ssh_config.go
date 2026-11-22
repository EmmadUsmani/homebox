package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// SSHConfigOptions selects the laptop configuration, its pins, or the local Include.
type SSHConfigOptions struct {
	Host       string
	KnownHosts bool
	Installer  bool
	Include    bool
}

// SSHConfig exports the configuration needed by OpenSSH clients.
func (s *Service) SSHConfig(ctx context.Context, options SSHConfigOptions) (string, error) {
	if options.Include {
		return sshx.IncludeLine(s.Store.SSHConfigPath())
	}
	boxes, err := s.Store.List()
	if err != nil {
		return "", err
	}
	if len(boxes) == 0 && !options.Installer {
		return "", nil
	}
	host := options.Host
	if !options.KnownHosts && host == "" && len(boxes) > 0 {
		if s.LocalHostName == nil {
			return "", errors.New("local hostname resolver is unavailable; specify --host")
		}
		host, err = s.LocalHostName(ctx)
		if err != nil {
			return "", err
		}
	}
	entries := make([]sshx.ConfigEntry, 0, len(boxes))
	for _, box := range boxes {
		if len(box.SSHHostKeys) == 0 {
			return "", fmt.Errorf("box %q has no pinned SSH host keys; run box start %s", box.Name, box.Name)
		}
		entries = append(entries, configEntry(box, host))
	}
	if options.Installer {
		return sshx.RenderInstaller(entries)
	}
	if options.KnownHosts {
		return sshx.RenderKnownHosts(entries)
	}
	return sshx.RenderConfig(entries, "~/.ssh/homebox_known_hosts")
}

func configEntry(box state.Box, host string) sshx.ConfigEntry {
	return sshx.ConfigEntry{Name: box.Name, Host: host, Port: box.SSHPort, User: box.User, HostKeys: box.SSHHostKeys}
}

func (s *Service) pinSSH(ctx context.Context, box *state.Box) (vm.Endpoint, error) {
	endpoint, keys, err := s.readSSHPins(ctx, box.Name)
	if err != nil {
		return vm.Endpoint{}, err
	}
	if len(box.SSHHostKeys) > 0 && !slices.Equal(box.SSHHostKeys, keys) {
		return vm.Endpoint{}, fmt.Errorf("SSH host keys changed for box %q; if intentional, run box repin %s", box.Name, box.Name)
	}
	return endpoint, s.saveSSHPins(box, endpoint, keys)
}

func (s *Service) readSSHPins(ctx context.Context, name string) (vm.Endpoint, []string, error) {
	endpoint, err := s.Driver.SSH(ctx, name)
	if err != nil {
		return vm.Endpoint{}, nil, err
	}
	if !filepath.IsAbs(endpoint.IdentityFile) {
		return vm.Endpoint{}, nil, fmt.Errorf("driver SSH identity for %q must be an absolute path", name)
	}
	keys, err := s.GuestSSH.HostKeys(ctx, endpoint)
	if err != nil {
		return vm.Endpoint{}, nil, fmt.Errorf("read SSH host keys for %s: %w", name, err)
	}
	if len(keys) == 0 {
		return vm.Endpoint{}, nil, fmt.Errorf("box %q returned no SSH host keys", name)
	}
	return endpoint, keys, nil
}

func (s *Service) saveSSHPins(box *state.Box, endpoint vm.Endpoint, keys []string) error {
	box.SSHHostKeys, box.SSHIdentityFile = keys, endpoint.IdentityFile
	if err := s.Store.Save(*box); err != nil {
		return err
	}
	if err := s.refreshLocalSSH(); err != nil {
		return fmt.Errorf("SSH pins saved, but local SSH files could not be refreshed; fix the error and retry: %w", err)
	}
	return nil
}

func (s *Service) refreshLocalSSH() error {
	boxes, err := s.Store.List()
	if err != nil {
		return err
	}
	var entries []sshx.ConfigEntry
	for _, box := range boxes {
		if len(box.SSHHostKeys) > 0 && box.SSHIdentityFile != "" {
			entry := configEntry(box, "127.0.0.1")
			entry.IdentityFile = box.SSHIdentityFile
			entries = append(entries, entry)
		}
	}
	config, err := sshx.RenderConfig(entries, s.Store.SSHKnownHostsPath())
	if err != nil {
		return err
	}
	knownHosts, err := sshx.RenderKnownHosts(entries)
	if err != nil {
		return err
	}
	return s.Store.SaveSSH(config, knownHosts)
}
