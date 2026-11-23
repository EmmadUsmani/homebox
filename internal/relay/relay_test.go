package relay

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// startRelay serves a relay to target and returns its address.
func startRelay(t *testing.T, target *net.TCPAddr) *net.TCPAddr {
	t.Helper()
	relay := Relay{Listen: "127.0.0.1:0", Target: target.String()}
	listener, err := relay.Bind()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return listener.Addr().(*net.TCPAddr)
}

func TestRelayRetriesUntilTheTargetListens(t *testing.T) {
	upstream, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	target := upstream.Addr().(*net.TCPAddr)
	upstream.Close()
	address := startRelay(t, target)

	client, err := net.DialTCP("tcp4", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	upstream, err = net.ListenTCP("tcp4", target)
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	upstream.SetDeadline(time.Now().Add(3 * time.Second))
	server, err := upstream.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.SetDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, len("request"))
	if _, err := io.ReadFull(server, buffer); err != nil || string(buffer) != "request" {
		t.Fatalf("upstream read %q: %v", buffer, err)
	}
	if _, err := server.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	buffer = make([]byte, len("response"))
	if _, err := io.ReadFull(client, buffer); err != nil || string(buffer) != "response" {
		t.Fatalf("client read %q: %v", buffer, err)
	}
}

// A laptop that goes away must end the guest-side connection too, even when
// the guest has nothing to send; otherwise the guest's sshd session lingers.
func TestRelayClosesTheTargetWhenTheClientGoes(t *testing.T) {
	upstream, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	address := startRelay(t, upstream.Addr().(*net.TCPAddr))

	client, err := net.DialTCP("tcp4", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	upstream.SetDeadline(time.Now().Add(3 * time.Second))
	server, err := upstream.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	waitForActive(t, 1)
	// Like Lima's forwarder, the target never reacts to the client's FIN.
	client.Close()
	waitForActive(t, 0)
}

func waitForActive(t *testing.T, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for active.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("%d forwarded connections, want %d", active.Load(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRelayClosesTheClientWhenTheTargetGoes(t *testing.T) {
	upstream, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	address := startRelay(t, upstream.Addr().(*net.TCPAddr))

	client, err := net.DialTCP("tcp4", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	upstream.SetDeadline(time.Now().Add(3 * time.Second))
	server, err := upstream.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("client connection still open after the target closed")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("client connection still open after the target closed")
	}
}
