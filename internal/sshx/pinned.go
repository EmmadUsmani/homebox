package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/crypto/ssh"
)

// ErrHostKeyChanged means the peer presented a key outside the saved pins.
var ErrHostKeyChanged = errors.New("SSH host key changed")

// DialPinned authenticates an in-process SSH transport using the saved box pins.
// The context bounds connection setup. The caller owns the returned transport.
func DialPinned(ctx context.Context, endpoint vm.Endpoint, pins []string) (*ssh.Client, error) {
	if ip := net.ParseIP(endpoint.Host); ip == nil || !ip.IsLoopback() {
		return nil, errors.New("expected a loopback SSH endpoint")
	}
	if len(pins) == 0 {
		return nil, errors.New("no pinned SSH host keys")
	}
	keys := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pin))
		if err != nil {
			return nil, fmt.Errorf("invalid saved SSH host key: %w", err)
		}
		keys[string(key.Marshal())] = struct{}{}
	}
	private, err := os.ReadFile(endpoint.IdentityFile)
	if err != nil {
		return nil, err
	}
	defer clear(private)
	signer, err := ssh.ParsePrivateKey(bytes.TrimSpace(private))
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User: endpoint.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, presented ssh.PublicKey) error {
			if _, ok := keys[string(presented.Marshal())]; !ok {
				return ErrHostKeyChanged
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	conn, channels, requests, err := ssh.NewClientConn(raw, address, config)
	if !stop() {
		_ = raw.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return ssh.NewClient(conn, channels, requests), nil
}
