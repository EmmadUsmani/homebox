package sshx

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

// ConfigEntry describes an SSH alias and its trusted guest keys.
type ConfigEntry struct {
	Name         string
	Host         string
	Port         int
	User         string
	IdentityFile string
	HostKeys     []string
}

func (entry ConfigEntry) alias() string { return "homebox-" + entry.Name }

var configWord = regexp.MustCompile(`^[a-zA-Z0-9_:.%-]+$`)

// RenderConfig renders pinned SSH aliases for the host or laptop.
func RenderConfig(entries []ConfigEntry, knownHosts string) (string, error) {
	knownHostsPath, err := configPath(knownHosts, true)
	if err != nil {
		return "", err
	}
	var config strings.Builder
	for index, entry := range entries {
		if !configWord.MatchString(entry.Name) || !configWord.MatchString(entry.Host) ||
			!configWord.MatchString(entry.User) || entry.Port < 1 || entry.Port > 65535 {
			return "", fmt.Errorf("invalid SSH endpoint for %q", entry.Name)
		}
		if index > 0 {
			config.WriteByte('\n')
		}
		fmt.Fprintf(&config, "Host %s\n  HostName %s\n  Port %d\n  User %s\n", entry.Name, entry.Host, entry.Port, entry.User)
		if entry.IdentityFile != "" {
			identity, err := configPath(entry.IdentityFile, true)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&config, "  IdentityFile %s\n  IdentitiesOnly yes\n", identity)
		}
		fmt.Fprintf(&config, "  HostKeyAlias %s\n  UserKnownHostsFile %s\n", entry.alias(), knownHostsPath)
		config.WriteString("  GlobalKnownHostsFile none\n  KnownHostsCommand none\n  StrictHostKeyChecking yes\n  VerifyHostKeyDNS no\n  UpdateHostKeys no\n  ForwardAgent no\n  ForwardX11 no\n")
	}
	return config.String(), nil
}

// RenderKnownHosts renders the stored pins under the same aliases as RenderConfig.
func RenderKnownHosts(entries []ConfigEntry) (string, error) {
	var output strings.Builder
	for _, entry := range entries {
		if !configWord.MatchString(entry.Name) {
			return "", fmt.Errorf("invalid SSH alias %q", entry.Name)
		}
		for _, line := range entry.HostKeys {
			key, ok := PublicKey(line)
			if !ok {
				return "", fmt.Errorf("invalid SSH host key for %q", entry.Name)
			}
			fmt.Fprintf(&output, "%s %s\n", entry.alias(), key)
		}
	}
	return output.String(), nil
}

func configPath(path string, expandPercent bool) (string, error) {
	if strings.ContainsAny(path, "\x00\r\n\"\\") || strings.Contains(path, "${") {
		return "", fmt.Errorf("path %q cannot be represented safely in SSH config", path)
	}
	if expandPercent {
		path = strings.ReplaceAll(path, "%", "%%")
	}
	return "\"" + path + "\"", nil
}

// LocalHostName resolves the Mac's mDNS hostname.
func (c Client) LocalHostName(ctx context.Context) (string, error) {
	result, err := c.Runner.Run(ctx, execx.Cmd{Name: "scutil", Args: []string{"--get", "LocalHostName"}})
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(result.Stdout))
	if name == "" {
		return "", fmt.Errorf("scutil returned an empty local hostname")
	}
	return name + ".local", nil
}
