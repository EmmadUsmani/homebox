package app

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// HostInitOptions updates LAN publishing and adds authorized key files.
type HostInitOptions struct {
	LAN                *bool
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
			key, ok := publicKey(line)
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

// publicKey recognizes the OpenSSH public key file format and discards comments.
func publicKey(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || strings.ContainsAny(line, "\x00\r") {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(data) < 4 {
		return "", false
	}
	length := binary.BigEndian.Uint32(data)
	if uint64(length) >= uint64(len(data)-4) || string(data[4:4+int(length)]) != fields[0] {
		return "", false
	}
	return fields[0] + " " + fields[1], true
}
