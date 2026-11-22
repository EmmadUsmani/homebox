//go:build darwin || linux

package vmnet

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/ethernet"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	gatewayIP   = "10.114.0.1"
	guestIP     = "10.114.0.21"
	gatewayMAC  = tcpip.LinkAddress("\x52\x55\x0a\x72\x00\x01")
	guestMAC    = tcpip.LinkAddress("\x52\x55\x0a\x72\x00\x15")
	networkNIC  = 1
	ethernetMTU = 1500
	frameMax    = ethernetMTU + header.EthernetMinimumSize
	frameQueue  = 1024
	frameBatch  = 16
)

var (
	subnet         = netip.MustParsePrefix("10.114.0.0/24")
	gatewayAddress = stackAddress(gatewayIP)
	guestAddress   = stackAddress(guestIP)
)

func serveGateway(ctx context.Context, listener net.Listener, logger *slog.Logger) error {
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	// Only a single attachment can feed this stack.
	listener.Close()
	logger.Info("guest network connected")
	return gatewaySession(ctx, conn, logger)
}

func gatewaySession(ctx context.Context, conn net.Conn, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	link := channel.New(frameQueue, frameMax, gatewayMAC)
	network := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4},
	})
	flows := newFlows(ctx, network, logger)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		conn.Close()
		workers.Wait()
		network.Close()
		flows.wait()
		network.Wait()
		link.Close()
	}()
	for _, err := range []tcpip.Error{
		network.CreateNIC(networkNIC, ethernet.New(link)),
		network.AddProtocolAddress(networkNIC, tcpip.ProtocolAddress{
			Protocol:          ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{Address: gatewayAddress, PrefixLen: subnet.Bits()},
		}, stack.AddressProperties{}),
		network.SetTransportProtocolOption(tcp.ProtocolNumber, &tcpip.TCPSendBufferSizeRangeOption{
			Min: tcp.MinBufferSize, Default: tcpBufferMax, Max: tcpBufferMax,
		}),
		network.SetTransportProtocolOption(tcp.ProtocolNumber, &tcpip.TCPReceiveBufferSizeRangeOption{
			Min: tcp.MinBufferSize, Default: tcpBufferMax, Max: tcpBufferMax,
		}),
		network.SetSpoofing(networkNIC, true),
		network.SetPromiscuousMode(networkNIC, true),
	} {
		if err != nil {
			return errors.New(err.String())
		}
	}
	route, err := tcpip.NewSubnet(stackAddress(subnet.Addr().String()), tcpip.MaskFromBytes(net.CIDRMask(subnet.Bits(), 32)))
	if err != nil {
		return err
	}
	network.SetRouteTable([]tcpip.Route{
		{Destination: route, NIC: networkNIC},
		{Destination: tcpip.AddressWithPrefix{Address: header.IPv4Broadcast, PrefixLen: 32}.Subnet(), NIC: networkNIC},
	})
	flows.install()

	var queue waiter.Queue
	dhcp, tcpErr := network.NewEndpoint(udp.ProtocolNumber, ipv4.ProtocolNumber, &queue)
	if tcpErr != nil {
		return errors.New(tcpErr.String())
	}
	dhcp.SocketOptions().SetBroadcast(true)
	if err := dhcp.Bind(tcpip.FullAddress{NIC: networkNIC, Port: 67}); err != nil {
		dhcp.Close()
		return errors.New(err.String())
	}
	dhcpConn := gonet.NewUDPConn(&queue, dhcp)
	stop := context.AfterFunc(ctx, func() {
		conn.Close()
		dhcpConn.Close()
	})
	defer func() {
		stop()
		dhcpConn.Close()
	}()
	workers.Go(func() { serveDHCP(dhcpConn) })
	workers.Go(func() {
		defer conn.Close()
		writer := bufio.NewWriterSize(conn, frameBatch*(4+frameMax))
		for {
			packet := link.ReadContext(ctx)
			if packet == nil {
				return
			}
			for count := 0; ; count++ {
				err := writeFrame(writer, packet)
				packet.DecRef()
				if err != nil {
					return
				}
				if count+1 == frameBatch {
					break
				}
				packet = link.Read()
				if packet == nil {
					break
				}
			}
			if err := writer.Flush(); err != nil {
				return
			}
		}
	})
	reader := bufio.NewReaderSize(conn, frameBatch*(4+frameMax))
	var data [frameMax]byte
	for {
		frame, err := readFrame(reader, &data)
		if err != nil {
			return err
		}
		if guestFrame(frame) {
			packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(frame)})
			link.InjectInbound(0, packet)
			packet.DecRef()
		}
	}
}

func stackAddress(address string) tcpip.Address {
	return tcpip.AddrFrom4(netip.MustParseAddr(address).As4())
}

func readFrame(reader io.Reader, data *[frameMax]byte) ([]byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(prefix[:])
	if size < header.EthernetMinimumSize || size > frameMax {
		return nil, fmt.Errorf("invalid Ethernet frame size %d", size)
	}
	frame := data[:size]
	_, err := io.ReadFull(reader, frame)
	return frame, err
}

func writeFrame(writer *bufio.Writer, packet *stack.PacketBuffer) error {
	frame := binary.BigEndian.AppendUint32(writer.AvailableBuffer(), uint32(packet.Size()))
	views, offset := packet.AsViewList()
	for view := views.Front(); view != nil; view = view.Next() {
		data := view.AsSlice()
		if offset >= len(data) {
			offset -= len(data)
			continue
		}
		frame = append(frame, data[offset:]...)
		offset = 0
	}
	_, err := writer.Write(frame)
	return err
}

func guestFrame(frame []byte) bool {
	if len(frame) < header.EthernetMinimumSize || len(frame) > frameMax {
		return false
	}
	ethernet := header.Ethernet(frame)
	if ethernet.SourceAddress() != guestMAC ||
		(ethernet.DestinationAddress() != gatewayMAC && ethernet.DestinationAddress() != header.EthernetBroadcastAddress) {
		return false
	}
	payload := frame[header.EthernetMinimumSize:]
	switch ethernet.Type() {
	case arp.ProtocolNumber:
		if len(payload) < header.ARPSize {
			return false
		}
		packet := header.ARP(payload)
		source := tcpip.AddrFromSlice(packet.ProtocolAddressSender())
		return packet.IsValid() && string(packet.HardwareAddressSender()) == string(guestMAC) &&
			(source == guestAddress || source == header.IPv4Any)
	case ipv4.ProtocolNumber:
		packet := header.IPv4(payload)
		if !packet.IsValid(len(payload)) {
			return false
		}
		if packet.SourceAddress() == guestAddress {
			return packet.DestinationAddress() != gatewayAddress || packet.TransportProtocol() != icmp.ProtocolNumber4
		}
		return packet.SourceAddress() == header.IPv4Any && packet.DestinationAddress() == header.IPv4Broadcast &&
			packet.TransportProtocol() == udp.ProtocolNumber
	default:
		return false
	}
}

func serveDHCP(conn *gonet.UDPConn) {
	var data [1500]byte
	for {
		size, _, err := conn.ReadFrom(data[:])
		if err != nil {
			return
		}
		request, err := dhcpv4.FromBytes(data[:size])
		if err != nil || request.OpCode != dhcpv4.OpcodeBootRequest || string(request.ClientHWAddr) != string(guestMAC) {
			continue
		}
		var response dhcpv4.MessageType
		switch request.MessageType() {
		case dhcpv4.MessageTypeDiscover:
			response = dhcpv4.MessageTypeOffer
		case dhcpv4.MessageTypeRequest:
			response = dhcpv4.MessageTypeAck
		default:
			continue
		}
		gateway := net.ParseIP(gatewayIP).To4()
		reply, err := dhcpv4.NewReplyFromRequest(request,
			dhcpv4.WithMessageType(response),
			dhcpv4.WithYourIP(net.ParseIP(guestIP).To4()),
			dhcpv4.WithOption(dhcpv4.OptServerIdentifier(gateway)),
			dhcpv4.WithOption(dhcpv4.OptRouter(gateway)),
			dhcpv4.WithOption(dhcpv4.OptSubnetMask(net.CIDRMask(subnet.Bits(), 32))),
			dhcpv4.WithOption(dhcpv4.OptIPAddressLeaseTime(time.Hour)))
		if err == nil {
			conn.WriteTo(reply.ToBytes(), &net.UDPAddr{IP: net.IPv4bcast, Port: 68})
		}
	}
}
