package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/EmmadUsmani/homebox/internal/displayproxy"
	"github.com/EmmadUsmani/homebox/internal/sshx"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/crypto/ssh"
)

// DesktopInstall adds the optional Ubuntu desktop packages to a running VM.
func (s *Service) DesktopInstall(ctx context.Context, name string) error {
	box, err := s.load(name)
	if err != nil {
		return err
	}
	if box.Profile != "ubuntu" {
		return fmt.Errorf("desktop installation is unavailable for profile %q", box.Profile)
	}
	script, err := s.Profiles.ReadScript("ubuntu/desktop.sh")
	if err != nil {
		return err
	}
	client, err := s.desktopSSH(ctx, name)
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	s.progress("Installing the desktop layer on %s", name)
	if err := runDesktopCommand(ctx, client, "sudo -n sh -s", strings.NewReader(string(script)), s.Progress); err != nil {
		return fmt.Errorf("install desktop on %s: %w", name, err)
	}
	return nil
}

// Desktop starts the guest session and serves it until interrupted or SSH is lost.
// With noOpen, publish receives the one-time browser URL instead of opening it.
func (s *Service) Desktop(ctx context.Context, name string, noOpen bool, publish func(string) error) error {
	client, err := s.desktopSSH(ctx, name)
	if err != nil {
		return err
	}
	defer client.Close()
	tunnel, closeTunnel := context.WithCancelCause(ctx)
	defer closeTunnel(nil)
	go func() {
		_ = client.Wait()
		closeTunnel(errors.New("desktop SSH connection lost"))
	}()
	go keepDesktopSSHAlive(tunnel, client)

	start, cancelStart := context.WithTimeout(tunnel, 35*time.Second)
	err = runDesktopCommand(start, client, displayproxy.GuestStart, nil, s.Progress)
	cancelStart()
	var exit *ssh.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitStatus() == 42:
		return fmt.Errorf("desktop layer missing on %s; run box desktop %s --install", name, name)
	case err != nil:
		return fmt.Errorf("start desktop on %s: %w", name, err)
	}

	proxy, err := displayproxy.Start(tunnel, func(ctx context.Context) (net.Conn, error) {
		return client.DialContext(ctx, "tcp", displayproxy.Endpoint)
	})
	if err != nil {
		return err
	}
	defer func() {
		closeTunnel(nil)
		_ = proxy.Wait()
	}()
	if noOpen {
		err = publish(proxy.BootstrapURL)
	} else if s.OpenURL != nil {
		if s.OpenURL(tunnel, proxy.BootstrapURL) != nil {
			return errors.New("could not open the desktop browser; try --no-open")
		}
	} else {
		return errors.New("browser opener unavailable; use --no-open")
	}
	if err != nil {
		return err
	}
	if err := proxy.Wait(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return context.Cause(tunnel)
}

func (s *Service) desktopSSH(ctx context.Context, name string) (*ssh.Client, error) {
	box, err := s.load(name)
	if err != nil {
		return nil, err
	}
	instance, err := s.Driver.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if instance.Status != vm.StatusRunning {
		return nil, fmt.Errorf("box %q is %s; run box start %s", name, instance.Status, name)
	}
	for _, forward := range box.Forwards {
		if forward.GuestPort == displayproxy.GuestPort {
			return nil, fmt.Errorf("remove the published guest port %d before using desktop on %s", displayproxy.GuestPort, name)
		}
	}
	endpoint, err := s.Driver.SSH(ctx, name)
	if err != nil {
		return nil, err
	}
	endpoint.IdentityFile = box.SSHIdentityFile
	client, err := sshx.DialPinned(ctx, endpoint, box.SSHHostKeys)
	if errors.Is(err, sshx.ErrHostKeyChanged) {
		return nil, fmt.Errorf("SSH host key changed for box %q; if intentional, run box repin %s", name, name)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s over pinned SSH: %w", name, err)
	}
	return client, nil
}

func runDesktopCommand(ctx context.Context, client *ssh.Client, command string, script io.Reader, output io.Writer) error {
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	session, err := client.NewSession()
	if err != nil {
		return errors.Join(err, ctx.Err())
	}
	defer session.Close()
	session.Stdin = script
	if output != nil {
		clean := &desktopOutput{out: output}
		session.Stdout, session.Stderr = clean, clean
	}
	return errors.Join(session.Run(command), ctx.Err())
}

type desktopOutput struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *desktopOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := io.WriteString(w.out, untrusted.Text(string(p)))
	return len(p), err
}

func keepDesktopSSHAlive(ctx context.Context, client *ssh.Client) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timer := time.AfterFunc(10*time.Second, func() { _ = client.Close() })
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			timer.Stop()
			if err != nil {
				return
			}
		}
	}
}
