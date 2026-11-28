package app

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// HostInitOptions configures LAN publishing, laptop keys and the local SSH Include.
type HostInitOptions struct {
	LAN                *bool
	SSHInclude         bool
	AuthorizedKeyFiles []string
}

// HostInit checks the driver and stores settings for creation and provisioning.
func (s *Service) HostInit(ctx context.Context, options HostInitOptions) error {
	var keys []string
	for _, path := range options.AuthorizedKeyFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var count int
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, ok := sshx.PublicKey(line)
			if !ok {
				return fmt.Errorf("invalid authorized key line in %s", path)
			}
			keys = append(keys, key)
			count++
		}
		if count == 0 {
			return fmt.Errorf("no authorized keys in %s", path)
		}
	}
	return s.Store.WithLock(func() error {
		if s.CheckHost != nil {
			if err := s.CheckHost(ctx); err != nil {
				return err
			}
		}
		s.progress("Checking %s driver", s.Driver.Name())
		instances, err := s.Driver.List(ctx)
		if err != nil {
			return fmt.Errorf("check VM driver: %w", err)
		}
		host, err := s.Store.LoadHost()
		if err != nil {
			return err
		}
		lanChanged := options.LAN != nil && *options.LAN != host.LAN
		if options.LAN != nil {
			host.LAN = *options.LAN
		}
		for _, key := range keys {
			if !slices.Contains(host.AuthorizedKeys, key) {
				host.AuthorizedKeys = append(host.AuthorizedKeys, key)
			}
		}
		if err := s.Store.SaveHost(host); err != nil {
			return err
		}
		if options.SSHInclude {
			if err := s.refreshLocalSSH(); err != nil {
				return err
			}
			if err := sshx.InstallInclude(s.Store.SSHConfigPath()); err != nil {
				return err
			}
		}
		s.progress("Host initialized (published ports bind to %s)", state.HostIP(host.LAN))
		if lanChanged || len(keys) > 0 {
			s.warnRunning(instances)
		}
		return nil
	})
}

// warnRunning names the running boxes that keep their old ports and keys
// until restarted: a running VM's listeners can't be rebound in place.
func (s *Service) warnRunning(instances []vm.Instance) {
	for _, instance := range instances {
		if instance.Status != vm.StatusRunning {
			continue
		}
		if _, err := s.Store.Load(instance.Name); err == nil {
			s.progress("%s is running with its old settings; apply them with: box stop %s && box start %s",
				instance.Name, instance.Name, instance.Name)
		}
	}
}
