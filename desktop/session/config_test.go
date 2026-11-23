package session

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/sshx"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func keyForTest(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer, private
}

func TestRemoteConfig(t *testing.T) {
	files := DefaultRemoteFiles(t.TempDir())
	source, err := sshx.RenderConfig([]sshx.ConfigEntry{
		{Name: "main", Host: "mini.local", Port: 2271, User: "guest"},
		{Name: "other", Host: "2001:db8::2", Port: 65535, User: "guest"},
	}, "~/.ssh/homebox_known_hosts")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := parseRemoteConfig([]byte("# generated\n"+source), files)
	if err != nil || hosts["main"] != (remoteHost{"mini.local", "2271", "guest", "homebox-main"}) ||
		hosts["other"].host != "2001:db8::2" {
		t.Fatalf("parse = %#v, %v", hosts, err)
	}
	for name, input := range map[string]string{
		"proxy":           source + "ProxyCommand anything\n",
		"match":           source + "Match all\n",
		"include":         "Include elsewhere\n" + source,
		"wildcard":        strings.Replace(source, "Host main", "Host *", 1),
		"multiple":        strings.Replace(source, "Host main", "Host main alternate", 1),
		"other file":      strings.Replace(source, "~/.ssh/homebox_known_hosts", "/tmp/keys", 1),
		"foreign pin":     strings.Replace(source, "homebox-main", "homebox-other", 1),
		"invalid port":    strings.Replace(source, "2271", "65536", 1),
		"forwarding":      strings.Replace(source, "ForwardAgent no", "ForwardAgent yes", 1),
		"duplicate alias": source + "\n" + source,
		"duplicate field": source + "User guest\n",
		"missing field":   strings.Replace(source, "  User guest\n", "", 1),
		"expansion":       strings.Replace(source, "mini.local", "%h", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRemoteConfig([]byte(input), files); err == nil {
				t.Fatal("accepted unexpected configuration")
			}
		})
	}
}

func TestPinnedHostKey(t *testing.T) {
	key, _ := keyForTest(t)
	other, _ := keyForTest(t)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key.PublicKey())))
	verify, err := pinnedKeys(remotePins([]byte("unrelated "+line+"\nhomebox-main "+line), "homebox-main"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verify("", nil, key.PublicKey()); err != nil {
		t.Fatal(err)
	}
	var changed *HostKeyChangedError
	if err := verify("", nil, other.PublicKey()); !errors.As(err, &changed) {
		t.Fatalf("want typed mismatch; got %v", err)
	}
	for _, pins := range [][]string{nil, {""}, {"ssh-ed25519 garbage"}, {"# " + line}} {
		if _, err := pinnedKeys(pins); err == nil {
			t.Fatalf("accepted invalid pins: %q", pins)
		}
	}
}

func TestAuthSelection(t *testing.T) {
	launchd := func(context.Context) string { return "/launchd/socket\n" }
	if got := agentSocket(t.Context(), func(string) string { return "/env/socket" }, launchd); got != "/env/socket" {
		t.Fatal(got)
	}
	if got := agentSocket(t.Context(), func(string) string { return "" }, launchd); got != "/launchd/socket" {
		t.Fatal(got)
	}
	if got := agentSocket(t.Context(), func(string) string { return "" }, func(context.Context) string { return "" }); got != "" {
		t.Fatal(got)
	}
	home := t.TempDir()
	if _, err := defaultKeys(home); err == nil {
		t.Fatal("missing identities accepted")
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, private := keyForTest(t)
	encrypted, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(encrypted), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultKeys(home); err == nil || !strings.Contains(err.Error(), "ssh-add") {
		t.Fatalf("want ssh-add guidance; got %v", err)
	}
	clearKey, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(clearKey), 0600); err != nil {
		t.Fatal(err)
	}
	if signers, err := defaultKeys(home); err != nil || len(signers) != 1 {
		t.Fatalf("want default identity; got %d, %v", len(signers), err)
	}
}

func TestRemoteAuthFallsBackAfterAgentKeyRejected(t *testing.T) {
	host, _ := keyForTest(t)
	wrong, agentPrivate := keyForTest(t)
	good, filePrivate := keyForTest(t)
	home := t.TempDir()
	pemKey, err := ssh.MarshalPrivateKey(filePrivate, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), pem.EncodeToMemory(pemKey), 0600); err != nil {
		t.Fatal(err)
	}

	// Use a short path even when TMPDIR plus the test name exceeds the Unix socket limit.
	dir, err := os.MkdirTemp("/tmp", "homebox-agent-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: agentPrivate}); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = agent.ServeAgent(keyring, conn)
			_ = conn.Close()
		}
	}()

	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	offers := make(chan string, 8)
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		offers <- ssh.FingerprintSHA256(key)
		if bytes.Equal(key.Marshal(), good.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, errors.New("key is not authorized")
	}}
	config.AddHostKey(host)
	go func() {
		for {
			conn, err := server.Accept()
			if err != nil {
				return
			}
			_, _, _, _ = ssh.NewServerConn(conn, config)
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	clientConfig := &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.FixedHostKey(host.PublicKey())}
	closeAuth := configureRemoteAuth(ctx, home, socket, clientConfig)
	defer closeAuth()
	client, err := dialSSH(ctx, server.Addr().String(), clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, key := range []ssh.Signer{wrong, good} {
		select {
		case got := <-offers:
			if want := ssh.FingerprintSHA256(key.PublicKey()); got != want {
				t.Fatalf("authentication offer %s, want %s", got, want)
			}
		case <-ctx.Done():
			t.Fatal("missing authentication offer")
		}
	}
	_ = client.Close()
	closeAuth()

	encrypted, err := ssh.MarshalPrivateKeyWithPassphrase(filePrivate, "", []byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), pem.EncodeToMemory(encrypted), 0600); err != nil {
		t.Fatal(err)
	}
	encryptedConfig := &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.FixedHostKey(host.PublicKey())}
	closeEncryptedAuth := configureRemoteAuth(ctx, home, socket, encryptedConfig)
	defer closeEncryptedAuth()
	rejected, err := dialSSH(ctx, server.Addr().String(), encryptedConfig)
	if !errors.Is(err, errEncryptedIdentity) {
		if rejected != nil {
			_ = rejected.Close()
		}
		t.Fatalf("want ssh-add guidance after rejecting the unrelated agent key, got %v", err)
	}

}
