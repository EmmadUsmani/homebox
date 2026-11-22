package doctor

import (
	"context"
	"errors"
	"net/netip"
	"runtime"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

// DefaultGateway returns the platform's IPv4 default gateway lookup.
func DefaultGateway(runner execx.Runner) func(context.Context) (netip.Addr, error) {
	return func(ctx context.Context) (netip.Addr, error) {
		if runtime.GOOS != "darwin" {
			return netip.Addr{}, errors.New("default gateway lookup is unavailable on this host")
		}
		return macGateway(ctx, runner)
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
