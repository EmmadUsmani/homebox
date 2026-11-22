//go:build darwin || linux

package vmnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/EmmadUsmani/homebox/internal/dnsfwd"
	"github.com/EmmadUsmani/homebox/internal/egress"
	"github.com/EmmadUsmani/homebox/internal/relay"
)

// Run serves the VM's networking until cancellation, SIGTERM, or service failure.
func Run(ctx context.Context, config Config) error {
	return run(ctx, config, nil)
}

func run(ctx context.Context, config Config, upstreams []string) (err error) {
	if err := config.validate(); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	pidFile, err := lockPIDFile(config.PIDFile)
	if err != nil {
		return err
	}
	defer func() {
		removePIDFile(pidFile)
		pidFile.Close()
	}()

	file, err := egress.OpenDaemonLog(config.LogFile)
	if err != nil {
		return err
	}
	defer file.Close()
	logger := slog.New(slog.NewJSONHandler(file, nil)).With("vm", config.Name)
	defer func() {
		if err != nil {
			logger.Error("VM network daemon failed", "error", err)
		}
	}()

	proxy, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: config.ProxyPort})
	if err != nil {
		return fmt.Errorf("bind proxy: %w", err)
	}
	defer proxy.Close()
	dns, err := dnsfwd.Bind(config.DNSPort, upstreams, 0)
	if err != nil {
		return fmt.Errorf("bind DNS: %w", err)
	}
	defer dns.Close()
	ssh := relay.Relay{
		Listen: endpoint(config.SSHPublish.IP, config.SSHPublish.Port),
		Target: endpoint("127.0.0.1", config.SSHTarget),
	}
	listener, err := ssh.Bind()
	if err != nil {
		return fmt.Errorf("bind SSH relay: %w", err)
	}
	defer listener.Close()
	if ctx.Err() != nil {
		return nil
	}
	if _, err := fmt.Fprintln(pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("publish readiness: %w", err)
	}
	logger.Info("VM network daemon ready")
	type service struct {
		name string
		run  func() error
	}
	services := []service{
		{"proxy", func() error { return egress.Serve(ctx, proxy, logger) }},
		{"DNS", func() error { return dns.Serve(ctx) }},
		{"SSH relay", func() error { return ssh.Serve(ctx, listener) }},
	}
	results := make(chan error, len(services))
	for _, service := range services {
		go func() {
			err := service.run()
			if err != nil {
				err = fmt.Errorf("%s: %w", service.name, err)
			}
			results <- err
		}()
	}
	for range services {
		result := <-results
		if err == nil {
			err = result
		}
		cancel()
	}
	logger.Info("VM network daemon stopped")
	return err
}
