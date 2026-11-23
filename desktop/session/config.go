// Package session owns the desktop app's pinned SSH transports and channels.
package session

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type VMRef struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

type RemoteFiles struct {
	Config     string
	KnownHosts string
	Home       string
}

func DefaultRemoteFiles(home string) RemoteFiles {
	return RemoteFiles{
		Config:     filepath.Join(home, ".ssh", "homebox_config"),
		KnownHosts: filepath.Join(home, ".ssh", "homebox_known_hosts"),
		Home:       home,
	}
}

type remoteHost struct {
	host, port, user, pin string
}

var word = regexp.MustCompile(`^[a-zA-Z0-9_:.%-]+$`)

var installerGuards = map[string]string{
	"GlobalKnownHostsFile": "none", "KnownHostsCommand": "none",
	"StrictHostKeyChecking": "yes", "VerifyHostKeyDNS": "no",
	"UpdateHostKeys": "no", "ForwardAgent": "no", "ForwardX11": "no",
}

func parseRemoteConfig(data []byte, files RemoteFiles) (map[string]remoteHost, error) {
	hosts := make(map[string]remoteHost)
	seen := make(map[string]bool)
	var alias string
	var host remoteHost
	finish := func() error {
		if alias == "" {
			return nil
		}
		if host.host == "" || host.port == "" || host.user == "" || host.pin == "" || !seen["UserKnownHostsFile"] {
			return fmt.Errorf("incomplete SSH config for %q", alias)
		}
		hosts[alias] = host
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		value := strings.TrimSpace(scanner.Text())
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		key, value, _ := strings.Cut(strings.ReplaceAll(value, "\t", " "), " ")
		value = strings.TrimSpace(value)
		invalid := fmt.Errorf("unsupported homebox SSH config at line %d (%s)", line, key)
		if key == "Host" {
			if err := finish(); err != nil {
				return nil, err
			}
			if !word.MatchString(value) || strings.Contains(value, "%") || hosts[value].host != "" {
				return nil, invalid
			}
			alias, host = value, remoteHost{}
			clear(seen)
			continue
		}
		if alias == "" || seen[key] {
			return nil, invalid
		}
		seen[key] = true
		if key == "UserKnownHostsFile" {
			// Compare against the two literal spellings the installer can represent.
			quoted := strconv.Quote(strings.ReplaceAll(files.KnownHosts, "%", "%%"))
			tilde := files.KnownHosts == DefaultRemoteFiles(files.Home).KnownHosts && value == `"~/.ssh/homebox_known_hosts"`
			if value != quoted && value != files.KnownHosts && !tilde {
				return nil, invalid
			}
			continue
		}
		if !word.MatchString(value) || strings.Contains(value, "%") {
			return nil, invalid
		}
		switch key {
		case "HostName":
			host.host = value
		case "Port":
			port, err := strconv.ParseUint(value, 10, 16)
			if err != nil || port == 0 {
				return nil, invalid
			}
			host.port = strconv.FormatUint(port, 10)
		case "User":
			host.user = value
		case "HostKeyAlias":
			if value != "homebox-"+alias {
				return nil, invalid
			}
			host.pin = value
		default:
			if installerGuards[key] != value {
				return nil, invalid
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hosts, finish()
}

type HostKeyChangedError struct{}

func (*HostKeyChangedError) Error() string { return "SSH host key changed" }

func pinnedKeys(lines []string) (ssh.HostKeyCallback, error) {
	if len(lines) == 0 {
		return nil, errors.New("no pinned SSH host keys")
	}
	keys := make([]ssh.PublicKey, 0, len(lines))
	for _, line := range lines {
		key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil || len(rest) != 0 || strings.Fields(line)[0] != key.Type() {
			return nil, errors.New("invalid pinned SSH host key")
		}
		keys = append(keys, key)
	}
	// Keep the default host-key algorithms so a replaced key of a different
	// algorithm still reaches the callback and is reported as a mismatch.
	return func(_ string, _ net.Addr, offered ssh.PublicKey) error {
		for _, key := range keys {
			if bytes.Equal(key.Marshal(), offered.Marshal()) {
				return nil
			}
		}
		return &HostKeyChangedError{}
	}, nil
}

func remotePins(data []byte, alias string) []string {
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == alias {
			keys = append(keys, strings.Join(fields[1:], " "))
		}
	}
	return keys
}

func privateKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

var errEncryptedIdentity = errors.New("SSH identity is encrypted; add the key to your SSH agent with ssh-add")

func defaultKeys(home string) ([]ssh.Signer, error) {
	var signers []ssh.Signer
	var encrypted *ssh.PassphraseMissingError
	hasEncrypted := false
	var readErr error
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		signer, err := privateKey(filepath.Join(home, ".ssh", name))
		switch {
		case err == nil:
			signers = append(signers, signer)
		case errors.Is(err, os.ErrNotExist):
		case errors.As(err, &encrypted):
			hasEncrypted = true
		default:
			readErr = fmt.Errorf("read SSH identity %s: %w", name, err)
		}
	}
	if hasEncrypted && len(signers) == 0 {
		return nil, errEncryptedIdentity
	}
	if len(signers) == 0 {
		if readErr != nil {
			return nil, readErr
		}
		return nil, errors.New("no SSH agent or default SSH identity found")
	}
	return signers, nil
}

func agentSocket(ctx context.Context, getenv func(string) string, launchd func(context.Context) string) string {
	if path := getenv("SSH_AUTH_SOCK"); path != "" {
		return path
	}
	return strings.TrimSpace(launchd(ctx))
}

func launchdSocket(ctx context.Context) string {
	data, _ := exec.CommandContext(ctx, "/bin/launchctl", "getenv", "SSH_AUTH_SOCK").Output()
	return string(data)
}

// remoteAuth resolves identities only after the server presents its pinned key.
func configureRemoteAuth(ctx context.Context, home, socket string, config *ssh.ClientConfig) func() {
	var conn net.Conn
	var fileErr error
	config.Auth = []ssh.AuthMethod{ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		var agentKeys []ssh.Signer
		var agentErr error
		if socket != "" {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			conn, agentErr = dialer.DialContext(ctx, "unix", socket)
			if agentErr == nil {
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				agentKeys, agentErr = agent.NewClient(conn).Signers()
				deadline, _ := ctx.Deadline()
				_ = conn.SetDeadline(deadline)
			}
		}
		var fileKeys []ssh.Signer
		fileKeys, fileErr = defaultKeys(home)
		seen := make(map[string]bool)
		var signers []ssh.Signer
		for _, keys := range [][]ssh.Signer{agentKeys, fileKeys} {
			for _, signer := range keys {
				key := string(signer.PublicKey().Marshal())
				if !seen[key] {
					signers = append(signers, signer)
					seen[key] = true
				}
			}
		}
		if len(signers) == 0 {
			if agentErr != nil {
				return nil, errors.Join(fmt.Errorf("read SSH agent identities: %w", agentErr), fileErr)
			}
			return nil, fileErr
		}
		return signers, nil
	})}
	config.AuthCallback = func(state *ssh.ClientAuthContext) (ssh.AuthMethod, error) {
		if errors.Is(fileErr, errEncryptedIdentity) && slices.Contains(state.TriedMethods, "publickey") {
			return nil, fileErr
		}
		return nil, nil
	}
	return func() {
		if conn != nil {
			_ = conn.Close()
		}
	}
}

func NewPool(local *app.Service, files RemoteFiles) *Pool {
	return newPool(func(ctx context.Context, ref VMRef) (*ssh.Client, error) {
		switch ref.Kind {
		case "local":
			box, err := local.Store.Load(ref.Name)
			if err != nil {
				return nil, err
			}
			endpoint, err := local.Driver.SSH(ctx, ref.Name)
			if err != nil {
				return nil, err
			}
			if endpoint.Host != "127.0.0.1" || !filepath.IsAbs(endpoint.IdentityFile) {
				return nil, errors.New("invalid local SSH endpoint")
			}
			verify, err := pinnedKeys(box.SSHHostKeys)
			if err != nil {
				return nil, err
			}
			key, err := privateKey(endpoint.IdentityFile)
			if err != nil {
				return nil, err
			}
			return dialSSH(ctx, net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port)), &ssh.ClientConfig{
				User: endpoint.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(key)}, HostKeyCallback: verify,
			})
		case "remote":
			return dialRemote(ctx, files, ref.Alias)
		default:
			return nil, errors.New("invalid VM reference")
		}
	})
}

func dialRemote(ctx context.Context, files RemoteFiles, alias string) (*ssh.Client, error) {
	data, err := os.ReadFile(files.Config)
	if err != nil {
		return nil, err
	}
	hosts, err := parseRemoteConfig(data, files)
	if err != nil {
		return nil, err
	}
	host, ok := hosts[alias]
	if !ok {
		return nil, fmt.Errorf("unknown homebox SSH alias %q", alias)
	}
	data, err = os.ReadFile(files.KnownHosts)
	if err != nil {
		return nil, err
	}
	verify, err := pinnedKeys(remotePins(data, host.pin))
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{User: host.user, HostKeyCallback: verify}
	socket := agentSocket(ctx, os.Getenv, launchdSocket)
	closeAuth := configureRemoteAuth(ctx, files.Home, socket, config)
	defer closeAuth()
	return dialSSH(ctx, net.JoinHostPort(host.host, host.port), config)
}
