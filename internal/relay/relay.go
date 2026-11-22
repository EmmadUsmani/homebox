// Package relay publishes a loopback TCP service through a TCP listener.
package relay

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Relay forwards TCP connections to a loopback service.
type Relay struct {
	Listen string
	Target string
}

// Bind opens the relay's listener before serving any traffic.
func (r Relay) Bind() (*net.TCPListener, error) {
	address, err := netip.ParseAddrPort(r.Listen)
	if err != nil || !address.Addr().Is4() {
		return nil, fmt.Errorf("invalid IPv4 relay listen address %q", r.Listen)
	}
	if _, err := r.target(); err != nil {
		return nil, err
	}
	return net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(address))
}

// Run binds and serves the relay until cancellation.
func (r Relay) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	listener, err := r.Bind()
	if err != nil {
		return err
	}
	return r.Serve(ctx, listener)
}

func (r Relay) target() (netip.AddrPort, error) {
	address, err := netip.ParseAddrPort(r.Target)
	if err != nil || address.Addr() != netip.MustParseAddr("127.0.0.1") || address.Port() == 0 {
		return netip.AddrPort{}, fmt.Errorf("relay target must be a nonzero port on 127.0.0.1")
	}
	return address, nil
}

// Serve handles connections on a bound listener and closes it on return.
func (r Relay) Serve(ctx context.Context, listener *net.TCPListener) error {
	defer listener.Close()
	target, err := r.target()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	for {
		client, err := listener.AcceptTCP()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		workers.Go(func() { forward(ctx, client, target) })
	}
}

func forward(ctx context.Context, client *net.TCPConn, target netip.AddrPort) {
	defer client.Close()
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	dialer := net.Dialer{}
	var upstream net.Conn
	for {
		conn, err := dialer.DialContext(dialCtx, "tcp4", target.String())
		if err == nil {
			upstream = conn
			break
		}
		select {
		case <-dialCtx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	server := upstream.(*net.TCPConn)
	defer server.Close()
	stop := context.AfterFunc(ctx, func() {
		client.Close()
		server.Close()
	})
	defer stop()
	var copies sync.WaitGroup
	copies.Go(func() { pipe(server, client) })
	pipe(client, server)
	copies.Wait()
}

func pipe(destination, source *net.TCPConn) {
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		source.Close()
		return
	}
	destination.CloseWrite()
}
