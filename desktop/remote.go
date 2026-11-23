package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/execx"
)

type RemoteHost struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

type Remote struct {
	source        app.RemoteSource
	inventoryPath string
	settingsPath  string
	mu            sync.Mutex
}

func newRemote(profiles app.Profiles) (*Remote, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	inventory := filepath.Join(home, ".ssh", "homebox_config")
	pins := filepath.Join(home, ".ssh", "homebox_known_hosts")
	config := os.Getenv("HOMEBOX_DESKTOP_SSH_CONFIG")
	if config != "" {
		inventory = config
	}
	if override := os.Getenv("HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS"); override != "" {
		pins = override
	}
	settings := filepath.Join(configDir, "homebox", "settings.json")
	if override := os.Getenv("HOMEBOX_DESKTOP_SETTINGS"); override != "" {
		settings = override
	}
	return &Remote{
		source:        app.RemoteSource{Runner: execx.OS{}, Profiles: profiles, ConfigPath: config, KnownHostsPath: pins},
		inventoryPath: inventory, settingsPath: settings,
	}, nil
}

func (remote *Remote) Hosts() ([]RemoteHost, error) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	return remote.hosts()
}

func (remote *Remote) hosts() ([]RemoteHost, error) {
	hosts := []RemoteHost{}
	file, err := os.Open(remote.inventoryPath)
	switch {
	case err == nil:
		defer file.Close()
		hosts, err = parseRemoteHosts(file)
		if err != nil {
			return nil, err
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	settings, err := readRemoteSettings(remote.settingsPath)
	if err != nil {
		return nil, err
	}
	for _, alias := range settings.RemoteHosts {
		if app.ValidRemoteAlias(alias) && !containsRemoteHost(hosts, alias) {
			hosts = append(hosts, RemoteHost{ID: alias, Name: alias})
		}
	}
	return hosts, nil
}

func (remote *Remote) AddHost(alias string) error {
	if !app.ValidRemoteAlias(alias) {
		return errors.New("Use 1–64 letters, digits, dots, hyphens or underscores, starting with a letter or digit.")
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	hosts, err := remote.hosts()
	if err != nil {
		return err
	}
	if containsRemoteHost(hosts, alias) {
		return nil
	}
	settings, err := readRemoteSettings(remote.settingsPath)
	if err != nil {
		return err
	}
	settings.RemoteHosts = append(settings.RemoteHosts, alias)
	return settings.save(remote.settingsPath)
}

func (remote *Remote) Check(ctx context.Context, alias string) (app.RemoteCheck, error) {
	return remote.source.Check(ctx, alias)
}

func containsRemoteHost(hosts []RemoteHost, alias string) bool {
	return slices.ContainsFunc(hosts, func(host RemoteHost) bool { return strings.EqualFold(host.Name, alias) })
}

func parseRemoteHosts(reader io.Reader) ([]RemoteHost, error) {
	hosts := []RemoteHost{}
	groupStart := 0
	inHost := false
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(strings.Replace(line, "=", " ", 1))
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			groupStart = len(hosts)
			inHost = true
			for _, alias := range fields[1:] {
				if app.ValidRemoteAlias(alias) && !containsRemoteHost(hosts, alias) {
					hosts = append(hosts, RemoteHost{ID: alias, Name: alias})
				}
			}
		case "match":
			inHost = false
		case "hostname":
			if inHost && len(fields[1]) <= 253 {
				for index := groupStart; index < len(hosts); index++ {
					if hosts[index].Host == "" {
						hosts[index].Host = fields[1]
					}
				}
			}
		}
	}
	return hosts, scanner.Err()
}
