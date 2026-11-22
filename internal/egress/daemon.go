package egress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// Config configures the loopback listener and optional daemon runtime files.
type Config struct {
	Listen   string
	LogFile  string
	PIDFile  string
	Lifeline func() bool // Returning false stops the daemon.
}

// Run serves the egress proxy until cancellation, SIGTERM, or lifeline loss.
func Run(ctx context.Context, config Config) error {
	if config.Listen == "" {
		config.Listen = "127.0.0.1:0"
	}
	address, err := netip.ParseAddrPort(config.Listen)
	if err != nil || address.Addr() != netip.MustParseAddr("127.0.0.1") {
		return fmt.Errorf("egress listener must be on 127.0.0.1")
	}
	listener, err := net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(address))
	if err != nil {
		return err
	}
	defer listener.Close()

	logger := slog.Default()
	if config.LogFile != "" {
		file, err := os.OpenFile(config.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		logger = slog.New(slog.NewJSONHandler(file, nil))
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	proxy := NewProxy(ProxyOptions{Logger: logger})
	proxy.denial = hostDenialReason
	server := &http.Server{
		Handler: proxy,
		// Only the header read is bounded; bodies and tunnels may be long.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	if config.PIDFile != "" {
		file, err := os.OpenFile(config.PIDFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer os.Remove(config.PIDFile)
		_, writeErr := fmt.Fprintln(file, strconv.Itoa(os.Getpid()))
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	}
	if config.Lifeline != nil {
		go watchLifeline(ctx, config.Lifeline, cancel)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if server.Shutdown(shutdown) != nil {
			server.Close()
		}
		proxy.stopTunnels()
	}()
	err = server.Serve(listener)
	cancel()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func watchLifeline(ctx context.Context, alive func() bool, cancel context.CancelFunc) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if !alive() {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func hostDenialReason(address netip.Addr) string {
	if reason := DenialReason(address); reason != "" {
		return reason
	}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return "cannot verify host interface addresses"
	}
	for _, entry := range interfaces {
		if addr, ok := entry.(*net.IPNet); ok {
			local, valid := netip.AddrFromSlice(addr.IP)
			if valid && local.Unmap() == address {
				return "target is assigned to a host interface"
			}
		}
	}
	return ""
}

// FreePort returns an available IPv4 loopback port without reserving it.
func FreePort() (int, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
