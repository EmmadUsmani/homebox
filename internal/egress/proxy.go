package egress

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const dialTimeout = 10 * time.Second

// Resolver resolves a hostname into candidate IP addresses.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// Dialer connects to a vetted numeric IP and port.
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// ProxyOptions overrides the proxy's dependencies and tunnel limit.
type ProxyOptions struct {
	Resolver   Resolver
	Dialer     Dialer
	Policy     func(netip.Addr) bool
	Logger     *slog.Logger
	MaxTunnels int
}

// Proxy forwards HTTP requests and TCP tunnels to vetted IP addresses.
type Proxy struct {
	resolver Resolver
	dialer   Dialer
	denial   func(netip.Addr) string
	logger   *slog.Logger
	slots    chan struct{}

	tunnelContext context.Context
	cancelTunnels context.CancelFunc
	tunnelMu      sync.Mutex
	stopping      bool
	tunnels       sync.WaitGroup
}

// NewProxy constructs a proxy. The default limit is 1024 concurrent tunnels:
// one dev VM running npm or a browser easily holds hundreds of connections.
func NewProxy(options ProxyOptions) *Proxy {
	if options.Resolver == nil {
		options.Resolver = net.DefaultResolver
	}
	if options.Dialer == nil {
		options.Dialer = &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.MaxTunnels <= 0 {
		options.MaxTunnels = 1024
	}
	denial := DenialReason
	if options.Policy != nil {
		denial = func(addr netip.Addr) string {
			if !options.Policy(addr) {
				return "address blocked by policy"
			}
			return ""
		}
	}
	tunnelContext, cancelTunnels := context.WithCancel(context.Background())
	return &Proxy{
		resolver: options.Resolver, dialer: options.Dialer, denial: denial,
		logger: options.Logger, slots: make(chan struct{}, options.MaxTunnels),
		tunnelContext: tunnelContext, cancelTunnels: cancelTunnels,
	}
}

type decision struct {
	target, reason string
	address        netip.Addr
	allowed        bool
	sent, received int64
}

// ServeHTTP handles an HTTP forward request or a CONNECT tunnel.
func (proxy *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target, host, port := destination(r)
	entry := decision{target: target}
	defer func() {
		result := "deny"
		if entry.allowed {
			result = "allow"
		}
		proxy.logger.Info("egress", "decision", result, "target", entry.target,
			"ip", entry.address.String(), "reason", entry.reason,
			"bytes_sent", entry.sent, "bytes_received", entry.received)
	}()
	if host == "" {
		entry.reason = "invalid proxy target"
		http.Error(w, entry.reason, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dialTimeout)
	defer cancel()
	var candidates []netip.Addr
	if addr, err := netip.ParseAddr(host); err == nil {
		candidates = []netip.Addr{addr}
	} else {
		var err error
		candidates, err = proxy.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			entry.reason = "DNS lookup failed"
			http.Error(w, entry.reason, http.StatusBadGateway)
			return
		}
	}
	local, _ := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
	entry.reason = "no target addresses"
	for _, candidate := range candidates {
		entry.address = candidate.WithZone("").Unmap()
		entry.reason = proxy.denial(entry.address)
		if !entry.address.IsValid() {
			entry.reason = "invalid address"
		}
		if local != nil && strconv.Itoa(local.Port) == port &&
			entry.address.WithZone("") == local.AddrPort().Addr().Unmap().WithZone("") {
			entry.reason = "proxy cannot connect to itself"
		}
		if entry.reason == "" {
			entry.allowed = true
			break
		}
	}
	if !entry.allowed {
		http.Error(w, "egress forbidden: "+entry.reason, http.StatusForbidden)
		return
	}

	pinned := net.JoinHostPort(entry.address.String(), port)
	if r.Method == http.MethodConnect {
		proxy.connect(ctx, w, pinned, &entry)
	} else {
		proxy.forward(w, r, pinned, &entry)
	}
}

func destination(r *http.Request) (target, host, port string) {
	if r.Method == http.MethodConnect {
		target = r.RequestURI
		host, port, _ = net.SplitHostPort(target)
	} else {
		target = r.URL.Host
		if r.URL.Scheme != "http" || r.URL.User != nil {
			return target, "", ""
		}
		host, port = r.URL.Hostname(), r.URL.Port()
		if port == "" {
			port = "80"
		}
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return target, "", ""
	}
	return target, host, strconv.FormatUint(number, 10)
}

func (proxy *Proxy) connect(ctx context.Context, w http.ResponseWriter, pinned string, entry *decision) {
	proxy.tunnelMu.Lock()
	if proxy.stopping || len(proxy.slots) == cap(proxy.slots) {
		proxy.tunnelMu.Unlock()
		entry.allowed, entry.reason = false, "tunnel capacity reached"
		http.Error(w, entry.reason, http.StatusServiceUnavailable)
		return
	}
	proxy.slots <- struct{}{}
	proxy.tunnels.Add(1)
	proxy.tunnelMu.Unlock()
	defer proxy.tunnels.Done()
	defer func() { <-proxy.slots }()

	upstream, err := proxy.dialer.DialContext(ctx, "tcp", pinned)
	if err != nil {
		entry.reason = "upstream dial failed"
		http.Error(w, entry.reason, http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	client, buffer, err := http.NewResponseController(w).Hijack()
	if err != nil {
		entry.reason = "client hijack failed"
		http.Error(w, entry.reason, http.StatusInternalServerError)
		return
	}
	defer client.Close()

	stop := context.AfterFunc(proxy.tunnelContext, func() {
		client.Close()
		upstream.Close()
	})
	defer stop()

	refreshDeadline := func() {
		deadline := time.Now().Add(time.Hour)
		client.SetDeadline(deadline)
		upstream.SetDeadline(deadline)
	}
	refreshDeadline()
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err == nil {
		err = buffer.Flush()
	}
	if err != nil {
		entry.reason = "client handshake failed"
		return
	}

	sent := make(chan int64, 1)
	// Drain bytes read along with the CONNECT headers, then read directly from TCP.
	input := io.MultiReader(io.LimitReader(buffer.Reader, int64(buffer.Reader.Buffered())), client)
	go func() { sent <- copyHalf(upstream, input, refreshDeadline) }()
	entry.received = copyHalf(client, upstream, refreshDeadline)
	entry.sent = <-sent
	entry.reason = "tunnel closed"
}

type tunnelWriter struct {
	io.Writer
	refreshDeadline func()
}

func (writer tunnelWriter) Write(data []byte) (int, error) {
	n, err := writer.Writer.Write(data)
	if n > 0 {
		writer.refreshDeadline()
	}
	return n, err
}

func copyHalf(dst net.Conn, src io.Reader, refreshDeadline func()) int64 {
	bytes, err := io.Copy(tunnelWriter{dst, refreshDeadline}, src)
	if half, ok := dst.(interface{ CloseWrite() error }); ok && err == nil {
		half.CloseWrite()
	} else {
		dst.Close()
	}
	return bytes
}

func (proxy *Proxy) stopTunnels() {
	proxy.tunnelMu.Lock()
	proxy.stopping = true
	proxy.tunnelMu.Unlock()
	proxy.cancelTunnels()
	proxy.tunnels.Wait()
}

func (proxy *Proxy) forward(w http.ResponseWriter, r *http.Request, pinned string, entry *decision) {
	// No overall deadline: plain-HTTP downloads (apt) can take many minutes.
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Host = out.URL.Host
	out.Close = false
	out.TransferEncoding = nil
	out.Trailer = nil
	stripHeaders(out.Header)
	var requestBody *countedBody
	if out.Body != nil && out.Body != http.NoBody {
		requestBody = &countedBody{ReadCloser: out.Body}
		out.Body = requestBody
		defer func() { entry.sent = requestBody.bytes.Load() }()
	}

	transport := http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			return proxy.dialer.DialContext(ctx, "tcp", pinned)
		},
		DisableCompression:    true,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	defer transport.CloseIdleConnections()
	resp, err := transport.RoundTrip(out)
	if err != nil {
		entry.reason = "upstream request failed"
		http.Error(w, entry.reason, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		entry.reason = "upstream protocol switch is unsupported"
		http.Error(w, entry.reason, http.StatusBadGateway)
		return
	}

	stripHeaders(resp.Header)
	for key, values := range resp.Header {
		w.Header()[key] = values
	}
	w.WriteHeader(resp.StatusCode)
	entry.received, err = io.Copy(w, resp.Body)
	entry.reason = "request completed"
	if err != nil {
		entry.reason = "response copy failed"
	}
}

type countedBody struct {
	io.ReadCloser
	bytes atomic.Int64
}

func (body *countedBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	body.bytes.Add(int64(n))
	return n, err
}

func stripHeaders(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, field := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(field))
		}
	}
	for _, field := range []string{
		"Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		headers.Del(field)
	}
	for field := range headers {
		if strings.HasPrefix(strings.ToLower(field), "proxy-") {
			delete(headers, field)
		}
	}
}
