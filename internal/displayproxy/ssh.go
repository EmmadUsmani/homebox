package displayproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"golang.org/x/crypto/ssh"
)

var ErrLayerMissing = errors.New("desktop layer missing")

// Open starts the guest desktop and opens its HTTP tunnel over pinned SSH.
func Open(ctx context.Context, client *ssh.Client, output io.Writer) (*Proxy, error) {
	return open(ctx, client, output, Start)
}

// OpenViewer applies additional resource restrictions for the native viewer.
func OpenViewer(ctx context.Context, client *ssh.Client) (*Proxy, error) {
	return open(ctx, client, nil, func(ctx context.Context, dial func(context.Context) (net.Conn, error)) (*Proxy, error) {
		return startWithPolicy(ctx, dial, bootstrapTTL, true)
	})
}

func open(ctx context.Context, client *ssh.Client, output io.Writer, startProxy func(context.Context, func(context.Context) (net.Conn, error)) (*Proxy, error)) (*Proxy, error) {
	start, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	err := runCommand(start, client, GuestStart, nil, output)
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.ExitStatus() == 42 {
		return nil, ErrLayerMissing
	}
	if err != nil {
		return nil, fmt.Errorf("start guest desktop: %w", err)
	}
	return startProxy(ctx, func(ctx context.Context) (net.Conn, error) {
		return client.DialContext(ctx, "tcp", Endpoint)
	})
}

// Install runs the Ubuntu desktop layer over the existing pinned connection.
func Install(ctx context.Context, client *ssh.Client, profiles interface {
	ReadScript(string) ([]byte, error)
}, profile string, output io.Writer) error {
	if profile != "ubuntu" {
		return fmt.Errorf("desktop installation is unavailable for profile %q", profile)
	}
	script, err := profiles.ReadScript("ubuntu/desktop.sh")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return runCommand(ctx, client, `. /etc/os-release; if [ "$ID" != ubuntu ]; then echo 'The desktop layer requires Ubuntu' >&2; exit 1; fi; exec sudo -n sh -s`, bytes.NewReader(script), output)
}

// SSH's channel open has no context. An abandoned open closes its channel when
// the peer eventually answers, without disrupting the other users of SSH.
func runCommand(ctx context.Context, client *ssh.Client, command string, script io.Reader, output io.Writer) error {
	if output == nil {
		return runSSHCommand(ctx, client, command, script, nil, nil)
	}
	clean := &commandOutput{out: output}
	return runSSHCommand(ctx, client, command, script, clean, clean)
}

func runSSHCommand(ctx context.Context, client *ssh.Client, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	type result struct {
		session *ssh.Session
		err     error
	}
	opened := make(chan result)
	go func() {
		session, err := client.NewSession()
		select {
		case opened <- result{session, err}:
		case <-ctx.Done():
			if session != nil {
				_ = session.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case reply := <-opened:
		if reply.err != nil {
			return reply.err
		}
		session := reply.session
		defer session.Close()
		session.Stdin, session.Stdout, session.Stderr = stdin, stdout, stderr
		done := make(chan error, 1)
		go func() { done <- session.Run(command) }()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return err
		}
	}
}

type commandOutput struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *commandOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := io.WriteString(w.out, untrusted.Text(string(p)))
	return len(p), err
}
