// Package dnsfwd relays DNS traffic from loopback to fixed upstream resolvers.
package dnsfwd

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	headerSize = 12
	maxMessage = 65535
)

// Forwarder owns a TCP and UDP listener on the same loopback port.
type Forwarder struct {
	tcp       *net.TCPListener
	udp       *net.UDPConn
	upstreams []netip.AddrPort
	timeout   time.Duration
}

// Bind opens both listeners. An empty upstream list uses public resolvers.
func Bind(port int, upstreams []string, timeout time.Duration) (*Forwarder, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid DNS port %d", port)
	}
	if len(upstreams) == 0 {
		upstreams = []string{"1.1.1.1:53", "9.9.9.9:53"}
	}
	addresses := make([]netip.AddrPort, 0, len(upstreams))
	for _, upstream := range upstreams {
		address, err := netip.ParseAddrPort(upstream)
		if err != nil || address.Port() == 0 {
			return nil, fmt.Errorf("invalid DNS upstream %q", upstream)
		}
		addresses = append(addresses, address)
	}
	if timeout <= 0 {
		timeout = time.Second
	}
	tcp, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return nil, err
	}
	address := tcp.Addr().(*net.TCPAddr)
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: address.IP, Port: address.Port})
	if err != nil {
		tcp.Close()
		return nil, err
	}
	return &Forwarder{tcp: tcp, udp: udp, upstreams: addresses, timeout: timeout}, nil
}

// Addr returns the common TCP and UDP address.
func (f *Forwarder) Addr() netip.AddrPort {
	return f.tcp.Addr().(*net.TCPAddr).AddrPort()
}

// Close releases both listeners.
func (f *Forwarder) Close() {
	f.tcp.Close()
	f.udp.Close()
}

// Serve forwards queries until canceled or either listener fails.
func (f *Forwarder) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, f.Close)
	defer stop()
	defer f.Close()
	results := make(chan error, 2)
	go func() { results <- f.serveUDP(ctx) }()
	go func() { results <- f.serveTCP(ctx) }()
	err := <-results
	canceled := ctx.Err() != nil
	cancel()
	<-results
	if canceled {
		return nil
	}
	return err
}

func (f *Forwarder) serveUDP(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	slots := make(chan struct{}, 128)
	buffer := make([]byte, maxMessage)
	for {
		size, client, err := f.udp.ReadFromUDPAddrPort(buffer)
		if err != nil {
			return err
		}
		if size < headerSize {
			continue
		}
		select {
		case slots <- struct{}{}:
			query := append([]byte(nil), buffer[:size]...)
			workers.Go(func() {
				defer func() { <-slots }()
				if answer := f.exchange(ctx, "udp", query); answer != nil {
					f.udp.WriteToUDPAddrPort(answer, client)
				}
			})
		default:
		}
	}
}

func (f *Forwarder) serveTCP(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	slots := make(chan struct{}, 128)
	for {
		client, err := f.tcp.AcceptTCP()
		if err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
			workers.Go(func() {
				defer func() { <-slots }()
				f.handleTCP(ctx, client)
			})
		default:
			client.Close()
		}
	}
}

func (f *Forwarder) handleTCP(ctx context.Context, client *net.TCPConn) {
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	for {
		client.SetReadDeadline(time.Now().Add(5 * time.Second))
		query, err := readFrame(client)
		if err != nil {
			return
		}
		answer := f.exchange(ctx, "tcp", query)
		if answer == nil {
			return
		}
		client.SetWriteDeadline(time.Now().Add(f.timeout))
		if err := writeFrame(client, answer); err != nil {
			return
		}
	}
}

func (f *Forwarder) exchange(ctx context.Context, network string, query []byte) []byte {
	for _, upstream := range f.upstreams {
		answer, err := exchange(ctx, network, upstream.String(), query, f.timeout)
		if err == nil && len(answer) >= headerSize && answer[0] == query[0] && answer[1] == query[1] {
			return answer
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil
}

func exchange(ctx context.Context, network, upstream string, query []byte, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, network, upstream)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if network == "tcp" {
		if err := writeFrame(conn, query); err != nil {
			return nil, err
		}
		return readFrame(conn)
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	answer := make([]byte, maxMessage)
	size, err := conn.Read(answer)
	return answer[:size], err
}

func readFrame(reader io.Reader) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint16(header[:])
	if size < headerSize {
		return nil, fmt.Errorf("invalid DNS message length %d", size)
	}
	message := make([]byte, size)
	_, err := io.ReadFull(reader, message)
	return message, err
}

func writeFrame(writer io.Writer, message []byte) error {
	frame := binary.BigEndian.AppendUint16(make([]byte, 0, len(message)+2), uint16(len(message)))
	_, err := writer.Write(append(frame, message...))
	return err
}
