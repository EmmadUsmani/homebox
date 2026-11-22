package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (fn resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return fn(ctx, network, host)
}

type dialerFunc func(context.Context, string, string) (net.Conn, error)

func (fn dialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return fn(ctx, network, address)
}

func TestConnectDenied(t *testing.T) {
	proxy := NewProxy(ProxyOptions{
		Logger: slog.New(slog.DiscardHandler),
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("fc00::1")}, nil
		}),
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			t.Error("blocked request reached the dialer")
			return nil, fmt.Errorf("blocked address")
		}),
	})
	for _, target := range []string{"127.0.0.1:22", "blocked.test:22", "[::ffff:127.0.0.1]:80"} {
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, httptest.NewRequest(http.MethodConnect, target, nil))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "egress forbidden") {
			t.Errorf("%s got %d %q", target, response.Code, response.Body.String())
		}
	}
}

func TestConnectMixedDNSAndHalfClose(t *testing.T) {
	upstream, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	payload := make(chan string, 1)
	go func() {
		conn, err := upstream.AcceptTCP()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		bytes, _ := io.ReadAll(conn)
		payload <- string(bytes)
		io.WriteString(conn, "pong")
	}()

	proxy := httptest.NewServer(NewProxy(ProxyOptions{
		Logger: slog.New(slog.DiscardHandler), MaxTunnels: 1,
		Resolver: resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
			if host != "mixed.test" {
				t.Errorf("resolved %q", host)
			}
			return []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("8.8.8.8")}, nil
		}),
		Dialer: dialerFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:22" {
				t.Errorf("dialed %q", address)
				return nil, fmt.Errorf("unexpected destination")
			}
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
		}),
	}))
	defer proxy.Close()

	client, buffered, status := connectRequest(t, proxy.Listener.Addr().String(), "mixed.test:22", "ping")
	defer client.Close()
	if status != http.StatusOK {
		t.Fatalf("CONNECT returned %d", status)
	}
	second, _, status := connectRequest(t, proxy.Listener.Addr().String(), "mixed.test:22", "")
	second.Close()
	if status != http.StatusServiceUnavailable {
		t.Errorf("CONNECT at capacity returned %d", status)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	bytes, err := io.ReadAll(buffered)
	if err != nil || string(bytes) != "pong" {
		t.Fatalf("read %q, %v", bytes, err)
	}
	if sent := <-payload; sent != "ping" {
		t.Errorf("upstream received %q", sent)
	}
}

func TestStopTunnelsClosesActiveConnection(t *testing.T) {
	upstream, peer := net.Pipe()
	defer peer.Close()
	handler := NewProxy(ProxyOptions{
		Logger: slog.New(slog.DiscardHandler),
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			return upstream, nil
		}),
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client, reader, status := connectRequest(t, server.Listener.Addr().String(), "8.8.8.8:22", "")
	defer client.Close()
	if status != http.StatusOK {
		t.Fatalf("CONNECT returned %d", status)
	}
	stopped := make(chan struct{})
	go func() {
		handler.stopTunnels()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("active tunnel did not stop")
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Errorf("closed tunnel returned %v", err)
	}
}

func TestForwardStripsHopHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != r.Context().Value(http.LocalAddrContextKey).(net.Addr).String() || r.URL.RequestURI() != "/test?q=1" {
			t.Errorf("upstream target %s%s", r.Host, r.URL.RequestURI())
		}
		for _, key := range []string{"X-Hop", "Proxy-Authorization", "Proxy-Unused", "Upgrade", "Te"} {
			if r.Header.Get(key) != "" {
				t.Errorf("upstream received %s", key)
			}
		}
		if r.Header.Get("X-End") != "request" {
			t.Error("upstream lost end-to-end header")
		}
		w.Header().Set("Connection", "X-Hop")
		w.Header().Set("X-Hop", "strip")
		w.Header().Set("Proxy-Unused", "strip")
		w.Header().Set("X-End", "response")
		io.WriteString(w, "forwarded")
	}))
	defer upstream.Close()
	handler := NewProxy(ProxyOptions{
		Logger: slog.New(slog.DiscardHandler),
		Policy: func(addr netip.Addr) bool { return addr.IsLoopback() },
	})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/test?q=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "irrelevant.test"
	req.Header.Set("Connection", "X-Hop, upgrade")
	for _, key := range []string{"X-Hop", "Proxy-Authorization", "Proxy-Unused", "Upgrade", "Te"} {
		req.Header.Set(key, "strip")
	}
	req.Header.Set("X-End", "request")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || response.Body.String() != "forwarded" {
		t.Fatalf("forward got %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-End") != "response" || response.Header().Get("X-Hop") != "" ||
		response.Header().Get("Proxy-Unused") != "" || response.Header().Get("Connection") != "" {
		t.Errorf("forwarded headers %v", response.Header())
	}
	client, _, status := connectRequest(t, proxy.Listener.Addr().String(), proxy.Listener.Addr().String(), "")
	client.Close()
	if status != http.StatusForbidden {
		t.Errorf("self-CONNECT returned %d", status)
	}
}

func connectRequest(t *testing.T, listener, target, payload string) (*net.TCPConn, *bufio.Reader, int) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", listener, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n%s", target, target, payload); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	return conn.(*net.TCPConn), reader, resp.StatusCode
}
