package egress

import (
	"errors"
	"net/netip"
	"testing"
)

func TestHostPolicy(t *testing.T) {
	networks := []netip.Prefix{
		netip.MustParsePrefix("8.8.8.0/24"),
		netip.MustParsePrefix("2600:9000:1234::/64"),
	}
	policy := hostPolicy{localNetworks: func() ([]netip.Prefix, error) { return networks, nil }}
	for _, test := range []struct{ target, want string }{
		{"8.8.8.42", "target is on a host interface network"},
		{"::ffff:8.8.8.42", "target is on a host interface network"},
		{"2600:9000:1234::42", "target is on a host interface network"},
		{"8.8.9.42", ""},
		{"2600:9000:1234:1::42", ""},
		{"127.0.0.1", "loopback"},
	} {
		if got := policy.denialReason(netip.MustParseAddr(test.target)); got != test.want {
			t.Errorf("%s: got %q, want %q", test.target, got, test.want)
		}
	}
	networks = nil
	if got := policy.denialReason(netip.MustParseAddr("8.8.8.42")); got != "" {
		t.Errorf("stale interface network blocked traffic: %s", got)
	}
	policy.localNetworks = func() ([]netip.Prefix, error) { return nil, errors.New("interface lookup failed") }
	if got := policy.denialReason(netip.MustParseAddr("8.8.8.42")); got != "cannot verify host interface networks" {
		t.Errorf("interface error produced %q", got)
	}
}
