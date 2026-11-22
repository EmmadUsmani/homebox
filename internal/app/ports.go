package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// PortOptions replaces, adds or removes published mappings. Remove takes guest ports.
type PortOptions struct {
	Set    []string
	Add    []string
	Remove []string
}

// Ports reports a box's forwards or updates them while the VM is stopped.
func (s *Service) Ports(ctx context.Context, name string, options PortOptions) ([]vm.PortForward, error) {
	if options.Set == nil && options.Add == nil && options.Remove == nil {
		box, err := s.load(name)
		return box.Forwards, err
	}
	var forwards []vm.PortForward
	err := s.Store.WithLock(func() error {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		instance, err := s.Driver.Get(ctx, name)
		if err != nil {
			return err
		}
		if instance.Status != vm.StatusStopped {
			return fmt.Errorf("stop %s before changing its ports", name)
		}
		host, err := s.Store.LoadHost()
		if err != nil {
			return err
		}
		previous := box.Forwards
		box.Forwards, err = applyPortOptions(previous, options, host.LAN)
		if err != nil {
			return err
		}
		if err := s.validatePorts(box); err != nil {
			return err
		}
		s.progress("Updating published ports for %s", name)
		if err := s.Driver.SetForwards(ctx, name, box.Forwards); err != nil {
			return err
		}
		if err := s.Store.Save(box); err != nil {
			return errors.Join(err, s.Driver.SetForwards(context.WithoutCancel(ctx), name, previous))
		}
		forwards = box.Forwards
		return s.refreshLocalSSH()
	})
	return forwards, err
}

func applyPortOptions(previous []vm.PortForward, options PortOptions, lan bool) ([]vm.PortForward, error) {
	forwards := slices.Clone(previous)
	if options.Set != nil {
		forwards = nil
	}
	for _, raw := range options.Remove {
		port, err := parsePort(raw)
		if err != nil {
			return nil, err
		}
		forwards = slices.DeleteFunc(forwards, func(f vm.PortForward) bool { return f.GuestPort == port })
	}
	for _, raw := range slices.Concat(options.Set, options.Add) {
		guest, host, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("invalid mapping %q: expected GUEST:HOST", raw)
		}
		guestPort, err := parsePort(guest)
		if err != nil {
			return nil, err
		}
		hostPort, err := parsePort(host)
		if err != nil {
			return nil, err
		}
		forwards = append(forwards, vm.PortForward{GuestPort: guestPort, HostPort: hostPort, HostIP: state.HostIP(lan)})
	}
	sort.Slice(forwards, func(i, j int) bool { return forwards[i].GuestPort < forwards[j].GuestPort })
	return forwards, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid TCP port %q", value)
	}
	return port, nil
}

func (s *Service) validatePorts(box state.Box) error {
	boxes, err := s.Store.List()
	if err != nil {
		return err
	}
	hosts := map[int]string{box.SSHPort: box.Name}
	for _, other := range boxes {
		if other.Name != box.Name {
			hosts[other.SSHPort] = other.Name
			for _, forward := range other.Forwards {
				hosts[forward.HostPort] = other.Name
			}
		}
	}
	if owner := hosts[box.SSHPort]; owner != box.Name {
		return fmt.Errorf("SSH host port %d is already assigned to %s", box.SSHPort, owner)
	}
	guests := map[int]bool{22: true}
	for _, forward := range box.Forwards {
		if guests[forward.GuestPort] {
			return fmt.Errorf("guest port %d already has a forward", forward.GuestPort)
		}
		if state.Reserved(forward.HostPort) {
			return fmt.Errorf("host port %d is reserved for box's own services", forward.HostPort)
		}
		if owner, used := hosts[forward.HostPort]; used {
			return fmt.Errorf("host port %d is already assigned to %s", forward.HostPort, owner)
		}
		guests[forward.GuestPort] = true
		hosts[forward.HostPort] = box.Name
	}
	return nil
}
