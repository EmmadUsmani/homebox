package egress

import (
	"net"
	"net/netip"
)

// hostPolicy checks directly attached networks. A router can still reflect
// traffic addressed to its public WAN IP back to a LAN port forward.
type hostPolicy struct {
	localNetworks func() ([]netip.Prefix, error)
}

func (p hostPolicy) denialReason(address netip.Addr) string {
	if reason := DenialReason(address); reason != "" {
		return reason
	}
	address = address.WithZone("").Unmap()
	networks, err := p.localNetworks()
	if err != nil {
		return "cannot verify host interface networks"
	}
	for _, network := range networks {
		if network.Contains(address) {
			return "target is on a host interface network"
		}
	}
	return ""
}

func hostInterfaceNetworks() ([]netip.Prefix, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var networks []netip.Prefix
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok {
				ip, valid := netip.AddrFromSlice(network.IP)
				bits, width := network.Mask.Size()
				if width == 32 {
					ip = ip.Unmap()
				}
				if !valid || width != ip.BitLen() {
					return nil, &net.AddrError{Err: "invalid host interface network", Addr: network.String()}
				}
				networks = append(networks, netip.PrefixFrom(ip, bits).Masked())
			}
		}
	}
	return networks, nil
}
