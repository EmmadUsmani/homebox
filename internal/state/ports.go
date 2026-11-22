package state

import "github.com/EmmadUsmani/homebox/internal/vm"

// SSHPort returns the host SSH port for a box index.
func SSHPort(index int) int { return 2200 + index }

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
