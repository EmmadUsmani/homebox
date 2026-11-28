//go:build darwin || linux

package vmnet

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

func TestReadFrame(t *testing.T) {
	var data [frameMax]byte
	for _, test := range []struct {
		name    string
		size    uint32
		body    int
		wantErr bool
	}{
		{"empty", 0, 0, true},
		{"too short", 13, 13, true},
		{"oversize", frameMax + 1, 0, true},
		{"uint32 maximum", ^uint32(0), 0, true},
		{"truncated", 14, 13, true},
		{"minimum Ethernet frame", 14, 14, false},
		{"maximum Ethernet frame", frameMax, frameMax, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := make([]byte, 4+test.body)
			binary.BigEndian.PutUint32(wire, test.size)
			frame, err := readFrame(bytes.NewReader(wire), &data)
			if (err != nil) != test.wantErr || (err == nil && len(frame) != test.body) {
				t.Fatalf("frame length %d, error %v", len(frame), err)
			}
		})
	}
	if _, err := readFrame(bytes.NewReader([]byte{0, 0, 5}), &data); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header: %v", err)
	}
}

func TestWriteFrameBatch(t *testing.T) {
	var wire bytes.Buffer
	writer := bufio.NewWriterSize(&wire, frameBatch*(4+frameMax))
	for i := 0; i < frameBatch; i++ {
		packet := stack.NewPacketBuffer(stack.PacketBufferOptions{
			ReserveHeaderBytes: 64,
			Payload:            buffer.MakeWithData(bytes.Repeat([]byte{byte(i)}, ethernetMTU)),
		})
		copy(packet.LinkHeader().Push(header.EthernetMinimumSize), []byte("EthernetHeader"))
		err := writeFrame(writer, packet)
		packet.DecRef()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	var data [frameMax]byte
	for i := 0; i < frameBatch; i++ {
		frame, err := readFrame(&wire, &data)
		if err != nil {
			t.Fatal(err)
		}
		if string(frame[:header.EthernetMinimumSize]) != "EthernetHeader" ||
			!bytes.Equal(frame[header.EthernetMinimumSize:], bytes.Repeat([]byte{byte(i)}, ethernetMTU)) {
			t.Fatalf("invalid frame %d", i)
		}
	}
	if wire.Len() != 0 {
		t.Fatal("unexpected trailing bytes")
	}
}

func FuzzGuestFrame(f *testing.F) {
	frame := make([]byte, header.EthernetMinimumSize+header.IPv4MinimumSize)
	header.Ethernet(frame).Encode(&header.EthernetFields{SrcAddr: guestMAC, DstAddr: gatewayMAC, Type: ipv4.ProtocolNumber})
	header.IPv4(frame[header.EthernetMinimumSize:]).Encode(&header.IPv4Fields{
		TotalLength: header.IPv4MinimumSize, TTL: 64, Protocol: uint8(tcp.ProtocolNumber),
		SrcAddr: stackAddress(guestIP), DstAddr: stackAddress("192.0.2.1"),
	})
	f.Add(frame)
	f.Add(frame[:header.EthernetMinimumSize])
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, wire []byte) { guestFrame(wire) })
}

func TestGuestFrameFiltersEthernetAndIP(t *testing.T) {
	frame := make([]byte, header.EthernetMinimumSize+header.IPv4MinimumSize)
	header.Ethernet(frame).Encode(&header.EthernetFields{SrcAddr: guestMAC, DstAddr: gatewayMAC, Type: ipv4.ProtocolNumber})
	payload := header.IPv4(frame[header.EthernetMinimumSize:])
	payload.Encode(&header.IPv4Fields{
		TotalLength: header.IPv4MinimumSize, TTL: 64, Protocol: uint8(tcp.ProtocolNumber),
		SrcAddr: guestAddress, DstAddr: stackAddress("192.0.2.1"),
	})
	if !guestFrame(frame) {
		t.Fatal("valid guest IPv4 frame was dropped")
	}
	ipv6 := bytes.Clone(frame)
	binary.BigEndian.PutUint16(ipv6[12:14], uint16(header.IPv6ProtocolNumber))
	if guestFrame(ipv6) {
		t.Fatal("IPv6 frame was accepted")
	}
	for _, test := range []struct {
		name  string
		index int
		value byte
	}{
		{"forged destination MAC", 0, 0x44},
		{"forged source MAC", 6, 0x44},
		{"IPv4 header too short", 14, 0x41},
		{"forged IPv4 source", 26, 0x44},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := bytes.Clone(frame)
			bad[test.index] = test.value
			if guestFrame(bad) {
				t.Fatal("invalid frame was accepted")
			}
		})
	}
}

func TestGuestFrameTransportAdmission(t *testing.T) {
	const greProtocol = 47
	for _, tc := range []struct {
		name                string
		protocol            uint8
		source, destination tcpip.Address
		fragment            bool
		want                bool
	}{
		{"TCP", uint8(tcp.ProtocolNumber), guestAddress, stackAddress("192.0.2.1"), false, true},
		{"UDP fragment", uint8(udp.ProtocolNumber), guestAddress, stackAddress("192.0.2.1"), true, true},
		{"unsupported protocol", greProtocol, guestAddress, stackAddress("192.0.2.1"), false, false},
		{"unsupported fragment", greProtocol, guestAddress, stackAddress("192.0.2.1"), true, false},
		{"gateway ICMP", uint8(icmp.ProtocolNumber4), guestAddress, gatewayAddress, false, false},
		{"initial DHCP", uint8(udp.ProtocolNumber), header.IPv4Any, header.IPv4Broadcast, false, true},
		{"non-DHCP broadcast protocol", uint8(tcp.ProtocolNumber), header.IPv4Any, header.IPv4Broadcast, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := make([]byte, header.EthernetMinimumSize+header.IPv4MinimumSize+8)
			header.Ethernet(frame).Encode(&header.EthernetFields{SrcAddr: guestMAC, DstAddr: gatewayMAC, Type: ipv4.ProtocolNumber})
			packet := header.IPv4(frame[header.EthernetMinimumSize:])
			packet.Encode(&header.IPv4Fields{
				TotalLength: header.IPv4MinimumSize + 8, TTL: 64, Protocol: tc.protocol,
				SrcAddr: tc.source, DstAddr: tc.destination,
			})
			if tc.fragment {
				packet.SetFlagsFragmentOffset(header.IPv4FlagMoreFragments, 8)
			}
			if got := guestFrame(frame); got != tc.want {
				t.Errorf("guestFrame = %t, want %t", got, tc.want)
			}
		})
	}
}
