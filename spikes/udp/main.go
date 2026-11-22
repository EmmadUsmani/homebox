// Experimental single-guest IPv4 gateway for Lima's QEMU stream attachment.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/containers/gvisor-tap-vsock/pkg/services/dhcp"
	"github.com/containers/gvisor-tap-vsock/pkg/services/forwarder"
	"github.com/containers/gvisor-tap-vsock/pkg/tap"
	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const (
	gateway     = "10.114.0.1"
	guestSubnet = "10.114.0.0/24"
	gatewayMAC  = "52:55:0a:72:00:01"
	guestIP     = "10.114.0.21"
	guestMAC    = "52:55:0a:72:00:15"
)

type packetHandler func(stack.TransportEndpointID, *stack.PacketBuffer) bool

// The upstream forwarders receive an empty translation table, so the checked
// endpoint is also the numeric address passed to the host socket.
func guard(protocol string, policy hostPolicy, next packetHandler) packetHandler {
	return func(id stack.TransportEndpointID, packet *stack.PacketBuffer) bool {
		address, _ := netip.AddrFromSlice(id.LocalAddress.AsSlice())
		if reason := policy.denialReason(address); reason != "" {
			log.Printf("deny %s %s: %s", protocol, address, reason)
			return true
		}
		return next(id, packet)
	}
}

func serve(socketPath string) error {
	endpoint, err := tap.NewLinkEndpoint(false, 1500, gatewayMAC, gateway, nil)
	if err != nil {
		return err
	}
	switcher := tap.NewSwitch(false)
	switcher.Connect(endpoint)
	endpoint.Connect(switcher)

	network := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4},
	})
	defer network.Close()
	const nic = 1
	if err := network.CreateNIC(nic, endpoint); err != nil {
		return errors.New(err.String())
	}
	address := tcpip.AddrFrom4(netip.MustParseAddr(gateway).As4())
	if err := network.AddProtocolAddress(nic, tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{Address: address, PrefixLen: 24},
	}, stack.AddressProperties{}); err != nil {
		return errors.New(err.String())
	}
	network.SetSpoofing(nic, true)
	network.SetPromiscuousMode(nic, true)
	_, subnet, err := net.ParseCIDR(guestSubnet)
	if err != nil {
		return err
	}
	route, err := tcpip.NewSubnet(tcpip.AddrFromSlice(subnet.IP), tcpip.MaskFromBytes(subnet.Mask))
	if err != nil {
		return err
	}
	network.SetRouteTable([]tcpip.Route{{Destination: route, NIC: nic}})

	var translationLock sync.Mutex
	policy := hostPolicy{localNetworks: hostInterfaceNetworks}
	tcpForward := forwarder.TCP(network, nil, &translationLock, false, 128, 5*time.Second)
	udpForward := forwarder.UDP(network, nil, &translationLock, false)
	icmpForward := forwarder.ICMP(network, nil, &translationLock)
	network.SetTransportProtocolHandler(tcp.ProtocolNumber, guard("tcp", policy, tcpForward.HandlePacket))
	network.SetTransportProtocolHandler(udp.ProtocolNumber, guard("udp", policy, udpForward.HandlePacket))
	network.SetTransportProtocolHandler(icmp.ProtocolNumber4, guard("icmp", policy, icmpForward.HandlePacket))

	pool, err := tap.NewIPPool(guestSubnet)
	if err != nil {
		return err
	}
	if err := pool.Reserve(gateway, gatewayMAC); err != nil {
		return err
	}
	if err := pool.Reserve(guestIP, guestMAC); err != nil {
		return err
	}
	dhcpServer, err := dhcp.New(&types.Configuration{
		Subnet: guestSubnet, GatewayIP: gateway, MTU: 1500,
	}, network, pool)
	if err != nil {
		return err
	}
	defer dhcpServer.Underlying.Close()
	go func() {
		if err := dhcpServer.Serve(); err != nil {
			log.Printf("DHCP stopped: %v", err)
		}
	}()

	if !filepath.IsAbs(socketPath) {
		return errors.New("socket path must be absolute")
	}
	dir, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		return err
	}
	if !dir.IsDir() || dir.Mode().Perm() != 0700 {
		return errors.New("socket parent must be a private 0700 directory")
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Print("gateway ready")
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Print("Lima connected")
	return switcher.Accept(context.Background(), conn, types.QemuProtocol)
}

func main() {
	socket := flag.String("socket", "", "absolute path to a new Unix socket in a private directory")
	flag.Parse()
	if err := serve(*socket); err != nil {
		log.Fatal(fmt.Errorf("gateway: %w", err))
	}
}
