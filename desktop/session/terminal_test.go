package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func serveTestShell(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "pty-req":
			var data struct {
				Term                      string
				Cols, Rows, Width, Height uint32
				Modes                     string
			}
			err := ssh.Unmarshal(request.Payload, &data)
			_ = request.Reply(err == nil && data.Term == "xterm-256color" && data.Cols == 80 && data.Rows == 24, nil)
		case "shell":
			_ = request.Reply(true, nil)
			go func() {
				defer channel.Close()
				reader := bufio.NewScanner(channel)
				for reader.Scan() {
					if reader.Text() == "exit" {
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						return
					}
					_, _ = fmt.Fprintln(channel, reader.Text())
				}
			}()
		case "window-change":
			var size struct{ Cols, Rows, Width, Height uint32 }
			if ssh.Unmarshal(request.Payload, &size) == nil {
				_, _ = fmt.Fprintf(channel, "size %d %d\n", size.Rows, size.Cols)
			}
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func TestTerminalLifecycle(t *testing.T) {
	key, _ := keyForTest(t)
	verify, err := pinnedKeys([]string{strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key.PublicKey())))})
	if err != nil {
		t.Fatal(err)
	}
	var connections atomic.Int32
	disconnected := make(chan struct{}, 1)
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(key)
	pool := newPool(func(ctx context.Context, _ VMRef) (*ssh.Client, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		defer listener.Close()
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			connections.Add(1)
			server, channels, requests, err := ssh.NewServerConn(conn, config)
			if err != nil {
				return
			}
			defer server.Close()
			defer func() { disconnected <- struct{}{} }()
			go ssh.DiscardRequests(requests)
			for channel := range channels {
				ch, requests, err := channel.Accept()
				if err == nil {
					go serveTestShell(ch, requests)
				}
			}
		}()
		return dialSSH(ctx, listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: verify})
	})
	term := NewTerminal(pool)
	defer term.ServiceShutdown()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ref := VMRef{Kind: "local", Name: "test"}
	one, two := term.Open(ctx, ref, 24, 80), term.Open(ctx, ref, 24, 80)
	if one.ID == "" || two.ID == "" || one.ID == two.ID || connections.Load() != 1 {
		t.Fatalf("expected separate channels on one transport: %+v / %+v (%d connections)", one, two, connections.Load())
	}
	term.Close(one.ID)
	if !term.Read(ctx, one.ID).Ended || !errors.Is(term.Write(one.ID, "no"), io.ErrClosedPipe) {
		t.Fatal("closed terminal still active")
	}
	read := func() string {
		for ctx.Err() == nil {
			output := term.Read(ctx, two.ID)
			if len(output.Data) > 0 || output.Ended {
				return string(output.Data)
			}
		}
		t.Fatal("timed out reading terminal")
		return ""
	}
	if err := term.Resize(two.ID, 33, 120); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "size 33 120\n" {
		t.Fatalf("resize output = %q", got)
	}
	if err := term.Write(two.ID, "still connected\n"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "still connected\n" {
		t.Fatalf("input echo = %q", got)
	}
	if err := term.Write(two.ID, "exit\n"); err != nil {
		t.Fatal(err)
	}
	for !term.Read(ctx, two.ID).Ended && ctx.Err() == nil {
	}
	select {
	case <-disconnected:
	case <-ctx.Done():
		t.Fatal("final terminal did not release transport")
	}
}

func TestTerminalOutputBackpressure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	chunks := make(chan []byte, 1)
	writer := outputWriter{done: ctx.Done(), chunks: chunks}
	if _, err := writer.Write(make([]byte, outputChunk)); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := writer.Write(make([]byte, 1))
		finished <- err
	}()
	select {
	case err := <-finished:
		t.Fatalf("writer bypassed full buffer: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("cancelled writer returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock a full buffer")
	}
	if got := len(<-chunks); got != outputChunk {
		t.Fatalf("buffered chunk length = %d", got)
	}
}

func TestDialTCPTriesNextResolvedAddress(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	conn, err := dialTCP(ctx, fmt.Sprintf("fixture.local:%d", port), func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "fixture.local" {
			t.Errorf("resolved host = %q", host)
		}
		return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 2)}, {IP: net.IPv4(127, 0, 0, 1)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := conn.RemoteAddr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Fatalf("connected to %s", got)
	}
}
