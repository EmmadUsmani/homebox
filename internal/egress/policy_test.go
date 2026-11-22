package egress

import (
	"net/netip"
	"testing"
)

func TestAllowedIPv4Boundaries(t *testing.T) {
	for _, test := range []struct {
		first, last                 string
		allowedBefore, allowedAfter bool
	}{
		{"0.0.0.0", "0.255.255.255", false, true},
		{"10.0.0.0", "10.255.255.255", true, true},
		{"100.64.0.0", "100.127.255.255", true, true},
		{"127.0.0.0", "127.255.255.255", true, true},
		{"169.254.0.0", "169.254.255.255", true, true},
		{"172.16.0.0", "172.31.255.255", true, true},
		{"192.0.0.0", "192.0.0.255", true, true},
		{"192.0.2.0", "192.0.2.255", true, true},
		{"192.168.0.0", "192.168.255.255", true, true},
		{"198.18.0.0", "198.19.255.255", true, true},
		{"198.51.100.0", "198.51.100.255", true, true},
		{"203.0.113.0", "203.0.113.255", true, true},
		{"224.0.0.0", "239.255.255.255", true, false},
		{"240.0.0.0", "255.255.255.255", false, false},
	} {
		t.Run(test.first, func(t *testing.T) {
			first, last := netip.MustParseAddr(test.first), netip.MustParseAddr(test.last)
			checkAddress(t, first.Prev(), test.allowedBefore)
			checkAddress(t, last.Next(), test.allowedAfter)
			for _, bound := range []netip.Addr{first, last} {
				checkAddress(t, bound, false)
				checkAddress(t, netip.MustParseAddr("::ffff:"+bound.String()), false)
				checkAddress(t, netip.MustParseAddr("::"+bound.String()), false)
				checkAddress(t, netip.MustParseAddr("64:ff9b::"+bound.String()), false)
			}
		})
	}
}

func TestAllowedIPv6(t *testing.T) {
	for _, test := range []struct {
		address string
		allowed bool
	}{
		{"::", false}, {"::1", false}, {"::1%zone", false},
		{"2001:db7:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:db8::", false}, {"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"2001:db9::", true},
		{"fbff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"fc00::", false}, {"fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"fe00::", true},
		{"fe7f:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"fe80::", false}, {"febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"fe80::1%zone", false},
		{"fec0::", false}, {"feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"ff00::", false}, {"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"::ffff:127.0.0.1%zone", false}, {"::ffff:8.8.8.8", true}, {"::8.8.8.8", true},
		{"64:ff9b::8.8.8.8", true}, {"2606:4700::1111", true}, {"2606:4700::1111%zone", true},
		{"2002:0a00:0001::", false}, {"2002:0808:0808::", true},
		{"2001:0:a00:1:0:0:f7f7:f7f7", false},
		{"2001:0:808:808:0:0:80ff:fffe", false},
		{"2001:0:808:808:0:0:f7f7:f7f7", true},
	} {
		t.Run(test.address, func(t *testing.T) {
			checkAddress(t, netip.MustParseAddr(test.address), test.allowed)
		})
	}
}

func checkAddress(t *testing.T, address netip.Addr, allowed bool) {
	t.Helper()
	if Allowed(address) != allowed || (DenialReason(address) == "") != allowed {
		t.Errorf("Allowed(%v) = %v, reason %q, want %v", address, Allowed(address), DenialReason(address), allowed)
	}
}
