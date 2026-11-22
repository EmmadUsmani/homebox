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
	Include    bool
}

// SSHConfig exports the configuration needed by OpenSSH clients.
func (s *Service) SSHConfig(ctx context.Context, options SSHConfigOptions) (string, error) {
	if options.Include {
		return sshx.IncludeLine(s.Store.SSHConfigPath())
	}
	boxes, err := s.Store.List()
	if err != nil || len(boxes) == 0 {
		return "", err
	}
	host := options.Host
	if !options.KnownHosts && host == "" {
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
	if options.KnownHosts {
		return sshx.RenderKnownHosts(entries)
	}
	return sshx.RenderConfig(entries, "~/.ssh/homebox_known_hosts")
}

func configEntry(box state.Box, host string) sshx.ConfigEntry {
	return sshx.ConfigEntry{Name: box.Name, Host: host, Port: box.SSHPort, User: box.User, HostKeys: box.SSHHostKeys}
}

func (s *Service) pinSSH(ctx context.Context, box *state.Box) (vm.Endpoint, error) {
	endpoint, err := s.Driver.SSH(ctx, box.Name)
	if err != nil {
		return vm.Endpoint{}, err
	}
	keys, err := s.GuestSSH.HostKeys(ctx, endpoint)
	if err != nil {
		return vm.Endpoint{}, fmt.Errorf("read SSH host keys for %s: %w", box.Name, err)
	}
	if len(keys) == 0 {
		return vm.Endpoint{}, fmt.Errorf("box %q returned no SSH host keys", box.Name)
	}
	if len(box.SSHHostKeys) > 0 && !slices.Equal(box.SSHHostKeys, keys) {
		return vm.Endpoint{}, fmt.Errorf("SSH host keys changed for box %q", box.Name)
	}
	if !filepath.IsAbs(endpoint.IdentityFile) {
		return vm.Endpoint{}, fmt.Errorf("driver SSH identity for %q must be an absolute path", box.Name)
	}
	box.SSHHostKeys, box.SSHIdentityFile = keys, endpoint.IdentityFile
	if err := s.Store.Save(*box); err != nil {
		return vm.Endpoint{}, err
	}
	return endpoint, s.refreshLocalSSH()
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
