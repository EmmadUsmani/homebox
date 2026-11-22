package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

var (
	guestUser       = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,30}$`)
	invalidUserRune = regexp.MustCompile(`[^a-z0-9_-]+`)
	configHost      = regexp.MustCompile(`^[a-zA-Z0-9_:.%-]+$`)
)

// NewOptions overrides the default profile or VM size.
type NewOptions struct {
	Name      string
	Profile   string
	CPUs      int
	MemoryGiB int
	DiskGiB   int
}

func defaultUser() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("determine current host user: %w", err)
	}
	_, name, hasDomain := strings.Cut(current.Username, "\\")
	if !hasDomain {
		name = current.Username
	}
	name = strings.Trim(invalidUserRune.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if name == "" || name == "root" {
		return "homebox", nil
	}
	if !guestUser.MatchString(name[:1]) {
		name = "u-" + name
	}
	return name[:min(len(name), 31)], nil
}

// New reserves a box record, creates its VM and provisions the guest.
func (s *Service) New(ctx context.Context, options NewOptions) error {
	if err := state.ValidateName(options.Name); err != nil {
		return err
	}
	if options.CPUs < 0 || options.MemoryGiB < 0 || options.DiskGiB < 0 {
		return errors.New("CPU, memory and disk sizes must be positive")
	}
	profileName := options.Profile
	if profileName == "" {
		profileName = guest.DefaultProfile
	}
	profile, err := s.Profiles.Get(profileName)
	if err != nil {
		return err
	}
	username, err := defaultUser()
	if err != nil {
		return err
	}
	return s.Store.WithLock(func() error {
		if _, err := s.Store.Load(options.Name); err == nil {
			return fmt.Errorf("box %q already exists", options.Name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := s.Driver.Get(ctx, options.Name); err == nil {
			return fmt.Errorf("driver already has an instance named %q", options.Name)
		} else if !errors.Is(err, vm.ErrNotFound) {
			return err
		}
		host, err := s.Store.LoadHost()
		if err != nil {
			return err
		}
		index, err := s.Store.NextIndex()
		if err != nil {
			return err
		}
		box := state.Box{
			Name: options.Name, Profile: profileName, Driver: s.Driver.Name(), Index: index,
			CPUs: sizeOrDefault(options.CPUs, 4), MemoryGiB: sizeOrDefault(options.MemoryGiB, 8),
			DiskGiB: sizeOrDefault(options.DiskGiB, 50), User: username, SSHPort: state.SSHPort(index),
			Forwards: state.DefaultForwards(index, host.LAN), CreatedAt: time.Now().UTC(),
		}
		if err := s.validatePorts(box); err != nil {
			return err
		}
		if err := s.Store.Save(box); err != nil {
			return err
		}
		return s.provision(ctx, box, profile)
	})
}

func sizeOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// Provision runs all the profile steps and can retry a partially created box.
func (s *Service) Provision(ctx context.Context, name string) error {
	return s.Store.WithLock(func() error {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		profile, err := s.Profiles.Get(box.Profile)
		if err != nil {
			return err
		}
		return s.provision(ctx, box, profile)
	})
}

func (s *Service) provision(ctx context.Context, box state.Box, profile guest.Profile) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("provision %s: %w; retry with box provision %s or remove with box rm %s",
				box.Name, err, box.Name, box.Name)
		}
	}()
	host, err := s.Store.LoadHost()
	if err != nil {
		return err
	}
	instance, err := s.Driver.Get(ctx, box.Name)
	if errors.Is(err, vm.ErrNotFound) {
		s.progress("Creating %s", box.Name)
		if err := s.Driver.Create(ctx, vm.Spec{
			Name: box.Name, Images: profile.Images, CPUs: box.CPUs, MemoryGiB: box.MemoryGiB,
			DiskGiB: box.DiskGiB, User: box.User, SSHPort: state.DriverSSHPort(box.Index),
			Forwards: box.Forwards,
			// The host isolation limits the gateway to the VM's proxy and DNS ports.
			HostGateway: true,
			// The confined host process can't resolve names for the guest, so the
			// driver's resolver is replaced by a placeholder until provisioning
			// points the guest at box's DNS forwarder (HOMEBOX_DNS).
			DNS: []string{"127.0.0.1"},
		}); err != nil {
			return err
		}
		instance, err = s.Driver.Get(ctx, box.Name)
	}
	if err != nil {
		return err
	}
	if err := s.start(ctx, &box, instance); err != nil {
		return err
	}
	endpoint, err := s.Driver.SSH(ctx, box.Name)
	if err != nil {
		return err
	}
	if len(host.AuthorizedKeys) > 0 {
		s.progress("Authorizing laptop keys for %s", box.Name)
		if err := s.GuestSSH.AuthorizeKeys(ctx, endpoint, host.AuthorizedKeys); err != nil {
			return err
		}
	}
	env := map[string]string{
		"HOMEBOX_PROXY_URL": box.ProxyURL, "HOMEBOX_NO_PROXY": box.NoProxy,
		"HOMEBOX_DNS": box.DNS, "HOMEBOX_USER": box.User,
	}
	for _, step := range profile.Steps {
		s.progress("Provisioning %s: %s", box.Name, step.Name)
		script, err := s.Profiles.ReadScript(step.Script)
		if err != nil {
			return err
		}
		if err := s.GuestSSH.RunScript(ctx, endpoint, script, env, step.Root, s.Progress, s.Progress); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
	}
	now := time.Now().UTC()
	box.Provisioned, box.ProvisionedAt = true, &now
	if err := s.Store.Save(box); err != nil {
		return err
	}
	s.progress("Provisioned %s", box.Name)
	return nil
}
