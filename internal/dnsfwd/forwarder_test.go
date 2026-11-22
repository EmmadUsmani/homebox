package dnsfwd

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestForwarder(t *testing.T) {
	loopback := net.IPv4(127, 0, 0, 1)
	upstream, err := Bind(0, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	tcp, udp := upstream.tcp, upstream.udp
	tcp.SetDeadline(time.Now().Add(3 * time.Second))
	udp.SetDeadline(time.Now().Add(3 * time.Second))
	silent, err := net.ListenUDP("udp4", &net.UDPAddr{IP: loopback})
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	forwarder, err := Bind(0, []string{silent.LocalAddr().String(), tcp.Addr().String()}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- forwarder.Serve(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	silent.SetDeadline(time.Now().Add(3 * time.Second))
	probes := make(chan []byte, 3)
	go func() {
		buffer := make([]byte, maxMessage)
		size, _, err := silent.ReadFromUDP(buffer)
		if err == nil {
			probes <- buffer[:size]
		}
	}()
	go func() {
		buffer := make([]byte, maxMessage)
		size, client, err := udp.ReadFromUDP(buffer)
		if err == nil {
			probes <- append([]byte(nil), buffer[:size]...)
			buffer[2] = 0x80
			udp.WriteToUDP(buffer[:size], client)
		}
	}()
	go func() {
		client, err := tcp.AcceptTCP()
		if err != nil {
			return
		}
		defer client.Close()
		client.SetDeadline(time.Now().Add(time.Second))
		query, err := readFrame(client)
		if err == nil {
			probes <- append([]byte(nil), query...)
			query[2] = 0x80
			writeFrame(client, query)
		}
	}()
	query := []byte{1, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	expected := append([]byte(nil), query...)
	expected[2] = 0x80
	for _, network := range []string{"udp", "tcp"} {
		answer, err := exchange(ctx, network, forwarder.Addr().String(), query, 2*time.Second)
		if err != nil || !bytes.Equal(answer, expected) {
			t.Fatalf("%s: answer %x, err %v", network, answer, err)
		}
	}
	for range 3 {
		select {
		case probe := <-probes:
			if !bytes.Equal(probe, query) {
				t.Errorf("upstream received %x, want %x", probe, query)
			}
		case <-time.After(time.Second):
			t.Fatal("missing upstream query")
		}
	}
}
