package sshx

import (
	"context"
	"fmt"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

// ConfigEntry describes an SSH alias on a laptop.
type ConfigEntry struct {
	Name string
	Host string
	Port int
	User string
}

// RenderConfig renders the laptop's SSH aliases.
func RenderConfig(entries []ConfigEntry) string {
	var config strings.Builder
	for index, entry := range entries {
		if index > 0 {
			config.WriteByte('\n')
		}
		fmt.Fprintf(&config, "Host %s\n  HostName %s\n  Port %d\n  User %s\n",
			entry.Name, entry.Host, entry.Port, entry.User)
	}
	return config.String()
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
