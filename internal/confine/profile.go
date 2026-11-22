// Package confine prepares host-side network restrictions for VM processes.
package confine

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// Rules describes the VM process's permitted network operations.
type Rules struct {
	RuntimeDir string
	ConnectTCP []int
	ConnectUDP []int
	BindTCP    []Bind
}

// Bind permits a TCP listener on loopback or on the LAN.
type Bind struct {
	Port int
	LAN  bool
}

func render(rules Rules) (string, error) {
	if !path.IsAbs(rules.RuntimeDir) || strings.ContainsFunc(rules.RuntimeDir, unicode.IsControl) {
		return "", fmt.Errorf("runtime directory must be an absolute path without control characters")
	}
	dir := path.Clean(rules.RuntimeDir)
	if dir == "/" {
		return "", fmt.Errorf("runtime directory cannot be the filesystem root")
	}
	socketPath := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(dir)
	var profile strings.Builder
	profile.WriteString("(version 1)\n(allow default)\n\n(deny network-outbound)\n(allow network-outbound\n")
	for _, remote := range []struct {
		protocol string
		ports    []int
	}{{"tcp", rules.ConnectTCP}, {"udp", rules.ConnectUDP}} {
		for _, port := range remote.ports {
			if !validPort(port) {
				return "", fmt.Errorf("invalid %s connect port %d", remote.protocol, port)
			}
			fmt.Fprintf(&profile, "  (remote %s \"localhost:%d\")\n", remote.protocol, port)
		}
	}
	fmt.Fprintf(&profile, "  (subpath \"%s\"))\n\n(deny network-bind)\n(allow network-bind\n", socketPath)
	for _, bind := range rules.BindTCP {
		if !validPort(bind.Port) {
			return "", fmt.Errorf("invalid TCP bind port %d", bind.Port)
		}
		host := "localhost"
		if bind.LAN {
			host = "*"
		}
		fmt.Fprintf(&profile, "  (local tcp \"%s:%d\")\n", host, bind.Port)
	}
	fmt.Fprintf(&profile, "  (subpath \"%s\"))\n", socketPath)
	return profile.String(), nil
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}
