package relay

import (
	"context"
	"io"
	"net"
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
