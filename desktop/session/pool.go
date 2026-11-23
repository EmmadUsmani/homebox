package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const setupTimeout = 10 * time.Second

type Pool struct {
	mu      sync.Mutex
	closed  bool
	entries map[VMRef]*connection
	dial    func(context.Context, VMRef) (*ssh.Client, error)
}

type connection struct {
	ready  chan struct{}
	cancel context.CancelFunc
	users  int
	client *ssh.Client
	err    error
}

func newPool(dial func(context.Context, VMRef) (*ssh.Client, error)) *Pool {
	return &Pool{entries: make(map[VMRef]*connection), dial: dial}
}

// Acquire leases a shared transport. Call release after closing all channels
// opened by this lease. Closing the last lease closes the transport.
func (p *Pool) Acquire(ctx context.Context, ref VMRef) (client *ssh.Client, release func(), err error) {
	switch ref.Kind {
	case "local":
		ref.Alias = ""
	case "remote":
		ref.Name = ""
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, nil, errors.New("SSH pool is closed")
	}
	entry := p.entries[ref]
	if entry == nil {
		setup, cancel := context.WithTimeout(context.Background(), setupTimeout)
		entry = &connection{ready: make(chan struct{}), cancel: cancel}
		p.entries[ref] = entry
		go p.connect(setup, ref, entry)
	}
	entry.users++
	p.mu.Unlock()

	release = sync.OnceFunc(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		entry.users--
		if entry.users == 0 {
			p.remove(ref, entry)
		}
	})
	select {
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	case <-entry.ready:
		if entry.err != nil {
			release()
			return nil, nil, entry.err
		}
		return entry.client, release, nil
	}
}

// remove runs with the pool lock held.
func (p *Pool) remove(ref VMRef, entry *connection) {
	if p.entries[ref] == entry {
		delete(p.entries, ref)
	}
	entry.cancel()
	if entry.client != nil {
		_ = entry.client.Close()
	}
}

func (p *Pool) connect(ctx context.Context, ref VMRef, entry *connection) {
	client, err := p.dial(ctx, ref)
	p.mu.Lock()
	if err == nil && p.entries[ref] != entry {
		_ = client.Close()
		err = context.Canceled
	}
	entry.client, entry.err = client, err
	close(entry.ready)
	if err != nil {
		p.remove(ref, entry)
	}
	p.mu.Unlock()
	if err == nil {
		waitForTransport(client)
		p.mu.Lock()
		p.remove(ref, entry)
		p.mu.Unlock()
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for ref, entry := range p.entries {
		p.remove(ref, entry)
	}
}

// Resolve IPv4 explicitly. macOS can return only IPv6 for a .local
// hostname on an unrestricted lookup even when the relay listens on IPv4.
func resolveSSHAddresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	v4, v4Err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	addresses := make([]net.IPAddr, 0, len(v4))
	seen := make(map[string]bool)
	add := func(address net.IPAddr) {
		key := address.String()
		if !seen[key] {
			seen[key] = true
			addresses = append(addresses, address)
		}
	}
	for _, address := range v4 {
		add(net.IPAddr{IP: address})
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	for _, address := range resolved {
		add(address)
	}
	if len(addresses) == 0 {
		return nil, errors.Join(v4Err, err)
	}
	return addresses, nil
}

func dialTCP(ctx context.Context, address string, resolve func(context.Context, string) ([]net.IPAddr, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	if _, err := netip.ParseAddr(host); err == nil {
		return dialer.DialContext(ctx, "tcp", address)
	}
	addresses, err := resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	var failures []error
	for index, target := range addresses {
		probe := ctx
		cancel := func() {}
		if deadline, bounded := ctx.Deadline(); bounded {
			remaining := time.Until(deadline)
			probe, cancel = context.WithTimeout(ctx, remaining/time.Duration(len(addresses)-index))
		}
		conn, err := dialer.DialContext(probe, "tcp", net.JoinHostPort(target.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		failures = append(failures, err)
	}
	return nil, fmt.Errorf("connect to %s: %w", address, errors.Join(failures...))
}

func dialSSH(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := dialTCP(ctx, address, resolveSSHAddresses)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	client, channels, requests, err := ssh.NewClientConn(conn, address, config)
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(client, channels, requests), nil
}

func waitForTransport(client *ssh.Client) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = client.Wait()
	}()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			timeout := time.AfterFunc(setupTimeout, func() { _ = client.Close() })
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			timeout.Stop()
			if err != nil {
				return
			}
		}
	}
}
