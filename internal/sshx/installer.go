package sshx

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed installer.sh
var installer string

// RenderInstaller packages the laptop aliases and their pins into a shell script.
func RenderInstaller(entries []ConfigEntry) (string, error) {
	if len(entries) == 0 {
		return "", fmt.Errorf("no boxes to export; create a box first")
	}
	config, err := RenderConfig(entries, "~/.ssh/homebox_known_hosts")
	if err != nil {
		return "", err
	}
	pins, err := RenderKnownHosts(entries)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if len(entry.HostKeys) == 0 {
			return "", fmt.Errorf("box %q has no pinned SSH host keys", entry.Name)
		}
	}
	// The validated renderers emit directives and key records, never a bare delimiter.
	const delimiter = "HOMEBOX_SSH_EOF"
	var script strings.Builder
	script.WriteString("#!/bin/sh\nset -eu\n\nwrite_payload() {\n")
	fmt.Fprintf(&script, "    cat > \"$stage/config\" <<'%s'\n%s%s\n", delimiter, config, delimiter)
	fmt.Fprintf(&script, "    cat > \"$stage/known_hosts\" <<'%s'\n%s%s\n", delimiter, pins, delimiter)
	script.WriteString("}\n\n")
	script.WriteString(installer)
	return script.String(), nil
}
