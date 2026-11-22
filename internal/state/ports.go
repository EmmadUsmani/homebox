package state

import "github.com/EmmadUsmani/homebox/internal/vm"

// Every host port a box uses derives from its index; see docs/architecture.md.

// SSHPort returns the published SSH port for a box index.
func SSHPort(index int) int { return 2200 + index }

// DriverSSHPort returns the loopback port of the driver's own SSH forward,
// which box's relay publishes as SSHPort.
func DriverSSHPort(index int) int { return 32200 + index }

// ProxyPort returns the loopback port of the box's egress proxy.
func ProxyPort(index int) int { return 23000 + index }

// DNSPort returns the loopback port of the box's DNS forwarder.
func DNSPort(index int) int { return 23100 + index }

// HostIP is the bind address for published ports.
func HostIP(lan bool) string {
	if lan {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// DefaultForwards returns the hundred HTTP forwards for a box index.
func DefaultForwards(index int, lan bool) []vm.PortForward {
	forwards := make([]vm.PortForward, 100)
	for offset := range forwards {
		forwards[offset] = vm.PortForward{
			GuestPort: 8000 + offset,
			HostPort:  10000 + 100*index + offset,
			HostIP:    HostIP(lan),
		}
	}
	return forwards
}
