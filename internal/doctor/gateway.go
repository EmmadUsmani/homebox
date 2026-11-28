package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"runtime"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

// DefaultGateway returns the platform's IPv4 default gateway lookup.
func DefaultGateway(runner execx.Runner) func(context.Context) (netip.Addr, error) {
	return func(ctx context.Context) (netip.Addr, error) {
		switch runtime.GOOS {
		case "darwin":
			return macGateway(ctx, runner)
		case "linux":
			result, err := runner.Run(ctx, execx.Cmd{Name: "ip", Args: []string{"-j", "-4", "route", "get", "1.1.1.1"}})
			if err != nil {
				return netip.Addr{}, err
			}
			var routes []struct{ Gateway netip.Addr }
			if err := json.Unmarshal(result.Stdout, &routes); err != nil {
				return netip.Addr{}, err
			}
			if len(routes) == 1 && routes[0].Gateway.Is4() {
				return routes[0].Gateway, nil
			}
			return netip.Addr{}, errors.New("no IPv4 gateway on the route to the internet")
		default:
			return netip.Addr{}, errors.New("default gateway lookup is unavailable on this host")
		}
	}
}

func macGateway(ctx context.Context, runner execx.Runner) (netip.Addr, error) {
	result, err := runner.Run(ctx, execx.Cmd{Name: "route", Args: []string{"-n", "get", "default"}})
	if err != nil {
		return netip.Addr{}, err
	}
	for line := range strings.SplitSeq(string(result.Stdout), "\n") {
		field, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		if field == "gateway" {
			if ip, err := netip.ParseAddr(strings.TrimSpace(value)); err == nil && ip.Is4() {
				return ip, nil
			}
		}
	}
	return netip.Addr{}, errors.New("no IPv4 default gateway found")
}
