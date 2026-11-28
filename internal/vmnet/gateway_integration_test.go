//go:build darwin || linux

package vmnet

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/egress"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"golang.org/x/net/dns/dnsmessage"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
)

const fixtureResolver = "8.8.4.4"

func TestGatewayFragmentedDNSAndInternalAnswers(t *testing.T) {
	hostAddress := netip.MustParseAddr("8.8.8.8")
	policy := egress.HostPolicy(func() ([]netip.Prefix, error) {
		return []netip.Prefix{netip.PrefixFrom(hostAddress, 24).Masked()}, nil
	})
	for _, tc := range []struct{ name, address, reason string }{
		{"private", "10.0.0.8", "private address"},
		{"loopback", "127.0.0.8", "loopback"},
		{"link-local", "169.254.0.8", "link-local"},
		{"CGNAT", "100.64.0.8", "carrier-grade NAT"},
		{"host interface", hostAddress.String(), "target is on a host interface network"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := dnsmessage.MustNewName("fixture.invalid.")
			query := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: 17},
				Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
			}
			address := netip.MustParseAddr(tc.address)
			answer := query
			answer.Response = true
			answer.Answers = []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
				Body:   &dnsmessage.AResource{A: address.As4()},
			}}
			var attempts atomic.Int32
			decisions := make(chan string, 4)
			wire, stop := fixtureGateway(t, gatewayEgress{
				denialReason: func(target netip.Addr) string {
					reason := policy(target)
					decisions <- reason
					return reason
				},
				dial: func(_ context.Context, protocol, target string) (net.Conn, error) {
					attempts.Add(1)
					if protocol != "udp4" || target != fixtureResolver+":53" {
						return nil, errors.New("unexpected upstream dial")
					}
					conn, resolver := net.Pipe()
					go func() {
						defer resolver.Close()
						resolver.SetDeadline(time.Now().Add(2 * time.Second))
						var packet [512]byte
						n, err := resolver.Read(packet[:])
						var received dnsmessage.Message
						if err != nil || received.Unpack(packet[:n]) != nil ||
							received.ID != query.ID || len(received.Questions) != 1 || received.Questions[0] != query.Questions[0] {
							return
						}
						payload, _ := answer.Pack()
						resolver.Write(payload)
					}()
					return conn, nil
				},
			})
			primeGatewayNeighbor(t, wire)
			payload, err := query.Pack()
			if err != nil {
				t.Fatal(err)
			}
			datagram := fixtureUDP(40000, 53, payload)
			resolver := stackAddress(fixtureResolver)
			// Send the final fragment first. The DNS server answers only after receiving the complete query.
			writeFixtureIPv4(t, wire, guestAddress, resolver, gatewayMAC, header.UDPProtocolNumber, 1, 16, 0, datagram[16:])
			writeFixtureIPv4(t, wire, guestAddress, resolver, gatewayMAC, header.UDPProtocolNumber, 1, 0, header.IPv4FlagMoreFragments, datagram[:16])
			packet := readFixtureUDP(t, wire)
			var reply dnsmessage.Message
			if err := reply.Unpack(packet.Payload()); err != nil || !reply.Response || reply.ID != query.ID || len(reply.Answers) != 1 {
				t.Fatalf("invalid reassembled DNS reply: %+v (%v)", reply, err)
			}
			a, ok := reply.Answers[0].Body.(*dnsmessage.AResource)
			if !ok || a.A != address.As4() {
				t.Fatalf("DNS answer = %+v, want %s", reply.Answers[0], address)
			}
			wantDecision(t, decisions, "")
			target := tcpip.AddrFrom4(a.A)
			writeFixtureIPv4(t, wire, guestAddress, target, gatewayMAC, header.UDPProtocolNumber, 2, 0, 0, fixtureUDP(40001, 8080, []byte("fixture")))
			if !address.IsLoopback() {
				wantDecision(t, decisions, tc.reason)
			}
			syn := header.TCP(make([]byte, header.TCPMinimumSize))
			syn.Encode(&header.TCPFields{SrcPort: 40002, DstPort: 8080, SeqNum: 1, DataOffset: header.TCPMinimumSize, Flags: header.TCPFlagSyn, WindowSize: 4096})
			syn.SetChecksum(^syn.CalculateChecksum(header.PseudoHeaderChecksum(header.TCPProtocolNumber, guestAddress, target, uint16(len(syn)))))
			writeFixtureIPv4(t, wire, guestAddress, target, gatewayMAC, header.TCPProtocolNumber, 3, 0, 0, syn)
			if address.IsLoopback() {
				// gVisor drops loopback destinations before transport dispatch.
				wire.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				var response [frameMax]byte
				if _, err := readFrame(wire, &response); !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("loopback traffic elicited a response: %v", err)
				}
				select {
				case reason := <-decisions:
					t.Fatalf("loopback reached transport policy: %s", reason)
				default:
				}
			} else {
				wantDecision(t, decisions, tc.reason)
			}
			stop()
			if got := attempts.Load(); got != 1 {
				t.Fatalf("upstream dial attempts = %d, want only the DNS query", got)
			}
		})
	}
}

func TestGatewayBroadcastAndMulticastDispatch(t *testing.T) {
	var attempts atomic.Int32
	wire, stop := fixtureGateway(t, gatewayEgress{
		denialReason: egress.DenialReason,
		dial: func(context.Context, string, string) (net.Conn, error) {
			attempts.Add(1)
			return nil, errors.New("unexpected upstream dial")
		},
	})
	for _, tc := range []struct {
		address tcpip.Address
		mac     tcpip.LinkAddress
	}{
		{header.IPv4Broadcast, header.EthernetBroadcastAddress},
		{stackAddress("10.114.0.255"), header.EthernetBroadcastAddress},
		{stackAddress("224.0.0.251"), gatewayMAC},
		{stackAddress("224.0.0.251"), "\x01\x00\x5e\x00\x00\xfb"},
	} {
		writeFixtureIPv4(t, wire, guestAddress, tc.address, tc.mac, header.UDPProtocolNumber, 1, 0, 0, fixtureUDP(40000, 5353, []byte("fixture")))
	}
	discover, err := dhcpv4.NewDiscovery(net.HardwareAddr(guestMAC))
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureIPv4(t, wire, header.IPv4Any, header.IPv4Broadcast, header.EthernetBroadcastAddress, header.UDPProtocolNumber, 2, 0, 0, fixtureUDP(68, 67, discover.ToBytes()))
	packet := readFixtureUDP(t, wire)
	reply, err := dhcpv4.FromBytes(packet.Payload())
	if err != nil || reply.MessageType() != dhcpv4.MessageTypeOffer || reply.TransactionID != discover.TransactionID || !reply.YourIPAddr.Equal(net.ParseIP(guestIP)) {
		t.Fatalf("expected a local DHCP offer, got %v (%v)", reply, err)
	}
	stop()
	if got := attempts.Load(); got != 0 {
		t.Fatalf("broadcast or multicast caused %d upstream dial attempts", got)
	}
}

func fixtureGateway(t *testing.T, upstream gatewayEgress) (net.Conn, func()) {
	t.Helper()
	guest, gateway := net.Pipe()
	guest.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		gatewaySession(t.Context(), gateway, slog.New(slog.NewTextHandler(io.Discard, nil)), upstream)
	}()
	stop := func() {
		guest.Close()
		<-done
	}
	t.Cleanup(stop)
	return guest, stop
}

func primeGatewayNeighbor(t *testing.T, wire net.Conn) {
	t.Helper()
	frame := make([]byte, header.EthernetMinimumSize+header.ARPSize)
	header.Ethernet(frame).Encode(&header.EthernetFields{SrcAddr: guestMAC, DstAddr: header.EthernetBroadcastAddress, Type: header.ARPProtocolNumber})
	packet := header.ARP(frame[header.EthernetMinimumSize:])
	packet.SetIPv4OverEthernet()
	packet.SetOp(header.ARPRequest)
	copy(packet.HardwareAddressSender(), guestMAC)
	copy(packet.ProtocolAddressSender(), guestAddress.AsSlice())
	copy(packet.ProtocolAddressTarget(), gatewayAddress.AsSlice())
	writeFixtureFrame(t, wire, frame)
	var reply [frameMax]byte
	if _, err := readFrame(wire, &reply); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureIPv4(t *testing.T, wire net.Conn, source, dest tcpip.Address, mac tcpip.LinkAddress, protocol tcpip.TransportProtocolNumber, id, offset uint16, flags uint8, payload []byte) {
	t.Helper()
	frame := make([]byte, header.EthernetMinimumSize+header.IPv4MinimumSize+len(payload))
	header.Ethernet(frame).Encode(&header.EthernetFields{SrcAddr: guestMAC, DstAddr: mac, Type: header.IPv4ProtocolNumber})
	packet := header.IPv4(frame[header.EthernetMinimumSize:])
	packet.Encode(&header.IPv4Fields{TotalLength: uint16(len(packet)), TTL: 64, ID: id, FragmentOffset: offset, Flags: flags, Protocol: uint8(protocol), SrcAddr: source, DstAddr: dest})
	packet.SetChecksum(^packet.CalculateChecksum())
	copy(packet.Payload(), payload)
	writeFixtureFrame(t, wire, frame)
}

func writeFixtureFrame(t *testing.T, wire net.Conn, frame []byte) {
	t.Helper()
	packet := binary.BigEndian.AppendUint32(nil, uint32(len(frame)))
	if _, err := wire.Write(append(packet, frame...)); err != nil {
		t.Fatal(err)
	}
}

func fixtureUDP(source, dest uint16, payload []byte) []byte {
	packet := header.UDP(make([]byte, header.UDPMinimumSize+len(payload)))
	packet.Encode(&header.UDPFields{SrcPort: source, DstPort: dest, Length: uint16(len(packet))})
	copy(packet.Payload(), payload)
	return packet
}

func readFixtureUDP(t *testing.T, wire net.Conn) header.UDP {
	t.Helper()
	var data [frameMax]byte
	frame, err := readFrame(wire, &data)
	if err != nil {
		t.Fatal(err)
	}
	packet := header.IPv4(frame[header.EthernetMinimumSize:])
	if header.Ethernet(frame).Type() != header.IPv4ProtocolNumber || !packet.IsValid(len(packet)) ||
		packet.TransportProtocol() != header.UDPProtocolNumber || len(packet.Payload()) < header.UDPMinimumSize {
		t.Fatalf("expected UDP response, got %x", frame)
	}
	return header.UDP(packet.Payload())
}

func wantDecision(t *testing.T, decisions <-chan string, want string) {
	t.Helper()
	select {
	case got := <-decisions:
		if got != want {
			t.Fatalf("policy decision = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no gateway policy decision")
	}
}
