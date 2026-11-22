//go:build darwin || linux

package vmnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/EmmadUsmani/homebox/internal/egress"
	"github.com/EmmadUsmani/homebox/internal/relay"
)

// Run serves the VM's networking until cancellation, SIGTERM, or service failure.
func Run(ctx context.Context, config Config) (err error) {
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

	listener, err := bindGateway(config.Socket)
	if err != nil {
		return fmt.Errorf("bind gateway: %w", err)
	}
	defer listener.Close()
	ssh := relay.Relay{
		Listen: endpoint(config.SSHPublish.IP, config.SSHPublish.Port),
		Target: endpoint("127.0.0.1", config.SSHTarget),
	}
	if ctx.Err() != nil {
		return nil
	}
	if _, err := fmt.Fprintln(pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("publish readiness: %w", err)
	}
	logger.Info("VM network daemon ready")
	results := make(chan error, 2)
	go func() {
		err := serveGateway(ctx, listener, logger)
		if ctx.Err() != nil || errors.Is(err, io.EOF) {
			err = nil
		}
		logger.Info("gateway session ended", "error", err)
		results <- err
	}()
	go func() {
		err := serveRelay(ctx, ssh, config.HostPIDFile)
		logger.Info("SSH relay stopped", "error", err)
		results <- err
	}()
	first := <-results
	cancel()
	logger.Info("VM network daemon stopping")
	return errors.Join(first, <-results)
}

func bindGateway(path string) (*net.UnixListener, error) {
	directory, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !directory.IsDir() || directory.Mode().Perm() != 0700 || directory.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("gateway socket directory must be owned by the daemon user and have mode 0700")
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode().Type() == os.ModeSocket:
		// The daemon PID lock excludes another owner of this private socket.
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	case err == nil:
		return nil, fmt.Errorf("gateway path exists and is not a socket")
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
