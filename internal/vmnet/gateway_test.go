//go:build darwin || linux

package vmnet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

func TestReadFrame(t *testing.T) {
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
			frame, err := readFrame(bytes.NewReader(wire))
			if (err != nil) != test.wantErr || (err == nil && len(frame) != test.body) {
				t.Fatalf("frame length %d, error %v", len(frame), err)
			}
		})
	}
	if _, err := readFrame(bytes.NewReader([]byte{0, 0, 5})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header: %v", err)
	}
}

func FuzzGuestFrame(f *testing.F) {
	frame := make([]byte, header.EthernetMinimumSize+header.IPv4MinimumSize)
	header.Ethernet(frame).Encode(&header.EthernetFields{SrcAddr: guestMAC, DstAddr: gatewayMAC, Type: ipv4.ProtocolNumber})
	header.IPv4(frame[header.EthernetMinimumSize:]).Encode(&header.IPv4Fields{
		TotalLength: header.IPv4MinimumSize, TTL: 64,
		SrcAddr: stackAddress(guestIP), DstAddr: stackAddress("192.0.2.1"),
	})
	f.Add(frame)
	f.Add(frame[:header.EthernetMinimumSize])
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, wire []byte) { guestFrame(wire) })
}
