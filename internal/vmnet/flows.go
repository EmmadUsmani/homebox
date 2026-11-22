//go:build darwin || linux

package vmnet

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/EmmadUsmani/homebox/internal/egress"
	neticmp "golang.org/x/net/icmp"
	netipv4 "golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
	"golang.org/x/time/rate"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	// 1,536 TCP flows × (512 KiB send + 128 KiB receive + 2 × 32 KiB
	// copy buffers) ≈ 1,056 MiB of buffer budget. UDP uses about 160 KiB
	// per flow (32 KiB receive queue + two 64 KiB buffers). Kernel socket
	// buffers and per-packet and endpoint overhead are separate.
	maxFlows            = 1536
	newFlowsPerSecond   = 500
	newFlowBurst        = maxFlows
	tcpReceiveBufferMax = 128 << 10
	tcpSendBufferMax    = 512 << 10
	tcpCopyBuffer       = 32 << 10
)

type flows struct {
	ctx      context.Context
	network  *stack.Stack
	logger   *slog.Logger
	slots    chan struct{}
	attempts *rate.Limiter
	denials  *rate.Limiter
	refusals *rate.Limiter
	workers  sync.WaitGroup
	mu       sync.Mutex
}

func newFlows(ctx context.Context, network *stack.Stack, logger *slog.Logger) *flows {
	return &flows{
		ctx: ctx, network: network, logger: logger,
		slots: make(chan struct{}, maxFlows), attempts: rate.NewLimiter(newFlowsPerSecond, newFlowBurst),
		denials: rate.NewLimiter(2, 8), refusals: rate.NewLimiter(1, 2),
	}
}

func (f *flows) reserve() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ctx.Err() != nil {
		return false
	}
	reason := "rate"
	if len(f.slots) == cap(f.slots) {
		reason = "capacity"
	} else if f.attempts.Allow() {
		f.slots <- struct{}{}
		f.workers.Add(1)
		return true
	}
	if f.refusals.Allow() {
		f.logger.Warn("egress flow refused", "reason", reason, "active", len(f.slots), "limit", cap(f.slots))
	}
	return false
}

func (f *flows) release() {
	<-f.slots
	f.workers.Done()
}

func (f *flows) wait() {
	// The session has canceled its context. Take the lock once to let an
	// admission already in progress finish before waiting.
	f.mu.Lock()
	f.mu.Unlock()
	f.workers.Wait()
}

func (f *flows) allowed(protocol string, id stack.TransportEndpointID) bool {
	if id.RemoteAddress != guestAddress || id.LocalAddress.Len() != 4 {
		return false
	}
	address := netip.AddrFrom4(id.LocalAddress.As4())
	reason := egress.HostDenialReason(address)
	if reason != "" && f.denials.Allow() {
		f.logger.Info("egress denied", "protocol", protocol, "destination", address, "reason", reason)
	}
	return reason == ""
}

func (f *flows) install() {
	tcpForward := tcp.NewForwarder(f.network, tcpReceiveBufferMax, maxFlows, f.forwardTCP)
	f.network.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForward.HandlePacket)
	f.network.SetTransportProtocolHandler(udp.ProtocolNumber, f.forwardUDP)
	f.network.SetTransportProtocolHandler(icmp.ProtocolNumber4, f.forwardICMP)
}

func (f *flows) dial(protocol string, id stack.TransportEndpointID) (net.Conn, error) {
	target := netip.AddrPortFrom(netip.AddrFrom4(id.LocalAddress.As4()), id.LocalPort)
	dialer := net.Dialer{Timeout: 5 * time.Second}
	return dialer.DialContext(f.ctx, protocol+"4", target.String())
}

func (f *flows) forwardTCP(request *tcp.ForwarderRequest) {
	if !f.reserve() {
		request.Complete(f.ctx.Err() == nil)
		return
	}
	defer f.release()
	if !f.allowed("tcp", request.ID()) {
		request.Complete(true)
		return
	}
	host, err := f.dial("tcp", request.ID())
	if err != nil {
		request.Complete(true)
		return
	}
	defer host.Close()
	if f.ctx.Err() != nil {
		request.Complete(false)
		return
	}
	var queue waiter.Queue
	endpoint, tcpErr := request.CreateEndpoint(&queue)
	request.Complete(false)
	if tcpErr != nil {
		return
	}
	guest := gonet.NewTCPConn(&queue, endpoint)
	bridgeFlow(f.ctx, guest, host, 5*time.Minute, tcpCopyBuffer)
}

func (f *flows) forwardUDP(id stack.TransportEndpointID, packet *stack.PacketBuffer) bool {
	if !f.reserve() {
		return true
	}
	if !f.allowed("udp", id) {
		f.release()
		return true
	}
	var queue waiter.Queue
	endpoint, err := udp.NewForwarderRequest(f.network, id, packet).CreateEndpoint(&queue)
	if err != nil {
		f.release()
		return true
	}
	guest := gonet.NewUDPConn(&queue, endpoint)
	go func() {
		defer f.release()
		defer guest.Close()
		host, err := f.dial("udp", id)
		if err == nil {
			bridgeFlow(f.ctx, guest, host, udpIdleTimeout(id.LocalPort), 65535)
		}
	}()
	return true
}

func udpIdleTimeout(port uint16) time.Duration {
	if port == 53 {
		return 2 * time.Second
	}
	return 30 * time.Second
}

func bridgeFlow(ctx context.Context, guest, host net.Conn, idle time.Duration, size int) {
	closeFlow := func() {
		guest.Close()
		host.Close()
	}
	defer closeFlow()
	stop := context.AfterFunc(ctx, closeFlow)
	defer stop()
	refresh := func() {
		deadline := time.Now().Add(idle)
		guest.SetDeadline(deadline)
		host.SetDeadline(deadline)
	}
	refresh()
	var copies sync.WaitGroup
	copyPackets := func(destination, source net.Conn) {
		data := make([]byte, size)
		for {
			read, err := source.Read(data)
			if read > 0 || err == nil {
				refresh()
				if _, err := destination.Write(data[:read]); err != nil {
					closeFlow()
					return
				}
			}
			if err != nil {
				if closer, ok := destination.(interface{ CloseWrite() error }); ok && err == io.EOF {
					closer.CloseWrite()
				} else {
					closeFlow()
				}
				return
			}
		}
	}
	copies.Go(func() { copyPackets(host, guest) })
	copyPackets(guest, host)
	copies.Wait()
}

func (f *flows) forwardICMP(id stack.TransportEndpointID, packet *stack.PacketBuffer) bool {
	request := header.ICMPv4(packet.TransportHeader().Slice())
	if len(request) < header.ICMPv4MinimumSize || request.Type() != header.ICMPv4Echo || request.Code() != 0 || !f.reserve() {
		return true
	}
	if !f.allowed("icmp", id) {
		f.release()
		return true
	}
	view := packet.Data().ToBuffer()
	payload := view.Flatten()
	view.Release()
	identifier, sequence := request.Ident(), request.Sequence()
	go func() {
		defer f.release()
		if f.echo(id.LocalAddress, payload) {
			f.replyEcho(id, identifier, sequence, payload)
		}
	}()
	return true
}

func (f *flows) echo(target tcpip.Address, payload []byte) bool {
	conn, err := dialEcho(target.As4())
	if err != nil {
		return false
	}
	defer conn.Close()
	stop := context.AfterFunc(f.ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	var token [4]byte
	rand.Read(token[:])
	identifier, sequence := int(binary.BigEndian.Uint16(token[:2])), int(binary.BigEndian.Uint16(token[2:]))
	message := neticmp.Message{Type: netipv4.ICMPTypeEcho, Body: &neticmp.Echo{ID: identifier, Seq: sequence, Data: payload}}
	data, err := message.Marshal(nil)
	if err != nil {
		return false
	}
	if _, err := conn.Write(data); err != nil {
		return false
	}
	if runtime.GOOS == "linux" {
		identifier = conn.LocalAddr().(*net.UDPAddr).Port
	}
	reply := make([]byte, len(data)+1)
	for {
		size, err := conn.Read(reply)
		if err != nil {
			return false
		}
		message, err := neticmp.ParseMessage(1, reply[:size])
		if err == nil && message.Type == netipv4.ICMPTypeEchoReply && message.Code == 0 {
			echo := message.Body.(*neticmp.Echo)
			if echo.ID == identifier && echo.Seq == sequence && bytes.Equal(echo.Data, payload) {
				return true
			}
		}
	}
}

// An outbound connected datagram socket needs no explicit bind. The kernel
// filters replies from other peers before the echo ID and payload checks.
func dialEcho(address [4]byte) (net.Conn, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, unix.IPPROTO_ICMP)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(fd), "vmnet-icmp4")
	defer file.Close()
	if runtime.GOOS == "darwin" {
		const ipStripHeader = 0x17
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, ipStripHeader, 1); err != nil {
			return nil, err
		}
	}
	if err := unix.Connect(fd, &unix.SockaddrInet4{Addr: address}); err != nil {
		return nil, err
	}
	return net.FileConn(file)
}

func (f *flows) replyEcho(id stack.TransportEndpointID, identifier, sequence uint16, payload []byte) {
	route, err := f.network.FindRoute(networkNIC, id.LocalAddress, id.RemoteAddress, ipv4.ProtocolNumber, false)
	if err != nil {
		return
	}
	defer route.Release()
	message := neticmp.Message{Type: netipv4.ICMPTypeEchoReply, Body: &neticmp.Echo{
		ID: int(identifier), Seq: int(sequence), Data: payload,
	}}
	data, marshalErr := message.Marshal(nil)
	if marshalErr != nil {
		return
	}
	reply := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: int(route.MaxHeaderLength()), Payload: buffer.MakeWithData(data),
	})
	defer reply.DecRef()
	reply.TransportProtocolNumber = icmp.ProtocolNumber4
	reply.NetworkProtocolNumber = ipv4.ProtocolNumber
	route.WritePacket(stack.NetworkHeaderParams{Protocol: icmp.ProtocolNumber4, TTL: 64}, reply)
}
