// Package egress defines the gateway's public IPv4 destination policy.
package egress

import "net/netip"

var blockedNetworks = []struct {
	prefix netip.Prefix
	reason string
}{
	{netip.MustParsePrefix("0.0.0.0/8"), "this network"},
	{netip.MustParsePrefix("10.0.0.0/8"), "private address"},
	{netip.MustParsePrefix("100.64.0.0/10"), "carrier-grade NAT"},
	{netip.MustParsePrefix("127.0.0.0/8"), "loopback"},
	{netip.MustParsePrefix("169.254.0.0/16"), "link-local"},
	{netip.MustParsePrefix("172.16.0.0/12"), "private address"},
	{netip.MustParsePrefix("192.0.0.0/24"), "protocol assignments"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation"},
	{netip.MustParsePrefix("192.168.0.0/16"), "private address"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation"},
	{netip.MustParsePrefix("224.0.0.0/4"), "multicast"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved or broadcast"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation"},
	{netip.MustParsePrefix("fc00::/7"), "unique-local"},
	{netip.MustParsePrefix("fe80::/10"), "link-local"},
	{netip.MustParsePrefix("fec0::/10"), "site-local"},
	{netip.MustParsePrefix("ff00::/8"), "multicast"},
}

var (
	ipv4Compatible = netip.MustParsePrefix("::/96")
	nat64          = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
	teredo         = netip.MustParsePrefix("2001::/32")
)

// Allowed reports whether the address passes the public egress policy.
func Allowed(addr netip.Addr) bool {
	return DenialReason(addr) == ""
}

// DenialReason returns the policy reason for blocking addr, or an empty string.
func DenialReason(addr netip.Addr) string {
	if !addr.IsValid() {
		return "invalid address"
	}
	addr = addr.WithZone("").Unmap()
	switch {
	case addr.IsUnspecified():
		return "unspecified"
	case addr.IsLoopback():
		return "loopback"
	}
	for _, network := range blockedNetworks {
		if network.prefix.Contains(addr) {
			return network.reason
		}
	}
	if addr.Is4() {
		return ""
	}

	bytes := addr.As16()
	switch {
	case ipv4Compatible.Contains(addr), nat64.Contains(addr):
		return DenialReason(netip.AddrFrom4([4]byte(bytes[12:16])))
	case sixToFour.Contains(addr):
		return DenialReason(netip.AddrFrom4([4]byte(bytes[2:6])))
	case teredo.Contains(addr):
		if reason := DenialReason(netip.AddrFrom4([4]byte(bytes[4:8]))); reason != "" {
			return reason
		}
		return DenialReason(netip.AddrFrom4([4]byte{^bytes[12], ^bytes[13], ^bytes[14], ^bytes[15]}))
	}
	return ""
}
