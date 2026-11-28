package relay

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayRetriesAndPreservesHalfClose(t *testing.T) {
	ip := net.IPv4(127, 0, 0, 1)
	upstream, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: ip})
	if err != nil {
		t.Fatal(err)
	}
	target := upstream.Addr().(*net.TCPAddr)
	upstream.Close()
	relay := Relay{Listen: "127.0.0.1:0", Target: target.String()}
	listener, err := relay.Bind()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Serve(ctx, listener) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
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
	if data, err := io.ReadAll(server); err != nil || string(data) != "request" {
		t.Fatalf("upstream read %q: %v", data, err)
	}
	if _, err := server.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	if err := server.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if data, err := io.ReadAll(client); err != nil || string(data) != "response" {
		t.Fatalf("client read %q: %v", data, err)
	}
}

func TestRelayDropsConnectionWhenTargetChangesDuringDial(t *testing.T) {
	upstream, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	var listenerID atomic.Uint64
	r := Relay{
		Listen: "127.0.0.1:0", Target: upstream.Addr().String(),
		TargetListenerID: func(netip.AddrPort) uint64 { return listenerID.Add(1) },
	}
	listener, err := r.Bind()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- r.Serve(ctx, listener) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	upstream.SetDeadline(time.Now().Add(time.Second))
	server, err := upstream.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.SetDeadline(time.Now().Add(time.Second))
	if data, err := io.ReadAll(server); err != nil || len(data) != 0 {
		t.Fatalf("unverified upstream read %q: %v", data, err)
	}
}

func TestRelayRejectsConnectionsAtCapacity(t *testing.T) {
	started := make(chan struct{}, maxConnections+1)
	r := Relay{
		Listen: "127.0.0.1:0", Target: "127.0.0.1:1",
		TargetDial: func(ctx context.Context) (net.Conn, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	listener, err := r.Bind()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- r.Serve(ctx, listener) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	connect := func() net.Conn {
		t.Helper()
		client, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		return client
	}
	for range maxConnections {
		connect()
	}
	deadline := time.After(time.Second)
	for range maxConnections {
		select {
		case <-started:
		case <-deadline:
			t.Fatal("relay did not admit its configured capacity")
		}
	}
	overflow := connect()
	overflow.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := overflow.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("overflow connection read = %v, want EOF", err)
	}
	select {
	case <-started:
		t.Fatal("relay dialed upstream for the overflow connection")
	default:
	}
}
