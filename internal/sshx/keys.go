package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

// HostKeys reads guest public host keys over a driver-authenticated connection.
func (c Client) HostKeys(ctx context.Context, ep vm.Endpoint) ([]string, error) {
	if ep.KnownHosts == "" {
		return nil, errors.New("driver SSH endpoint has no pinned host keys")
	}
	result, err := c.Run(ctx, ep, "sh", "-c", "cat /etc/ssh/ssh_host_*_key.pub")
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		key, ok := PublicKey(line)
		if !ok {
			return nil, fmt.Errorf("guest returned an invalid public host key")
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return slices.Compact(keys), nil
}

// PublicKey recognizes the OpenSSH public key file format and discards comments.
func PublicKey(line string) (string, bool) {
	key, _, ok := parsePublicKey(line)
	return key, ok
}

// Fingerprint formats a public key fingerprint as OpenSSH does.
func Fingerprint(line string) (string, error) {
	key, data, ok := parsePublicKey(line)
	if !ok {
		return "", errors.New("invalid SSH public key")
	}
	kind, _, _ := strings.Cut(key, " ")
	digest := sha256.Sum256(data)
	return kind + " SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:]), nil
}

func parsePublicKey(line string) (string, []byte, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || strings.ContainsAny(line, "\x00\r") {
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(data) < 4 {
		return "", nil, false
	}
	length := binary.BigEndian.Uint32(data)
	if uint64(length) >= uint64(len(data)-4) || string(data[4:4+int(length)]) != fields[0] {
		return "", nil, false
	}
	return fields[0] + " " + fields[1], data, true
}
