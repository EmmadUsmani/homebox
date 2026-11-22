// Package e2e exercises the box CLI against a real VM.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/state"
)

func TestBoxLifecycle(t *testing.T) {
	if os.Getenv("HOMEBOX_E2E") != "1" {
		t.Skip("set HOMEBOX_E2E=1 to boot a real VM")
	}

	const name = "e2e1"
	// HOMEBOX_E2E_INDEX lets concurrent runs use separate host ports.
	index := 20
	if value := os.Getenv("HOMEBOX_E2E_INDEX"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("HOMEBOX_E2E_INDEX: %v", err)
		}
		index = parsed
	}
	// State lives in the home directory, like the default ~/.homebox, because
	// the host sandboxes treat it differently from /tmp. The path stays short
	// enough for the driver's Unix sockets.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".hb-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	cleanupOK := true
	t.Cleanup(func() {
		if cleanupOK {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
		}
	})
	root := filepath.Join(dir, "state")
	t.Setenv("HOMEBOX_HOME", root)
	t.Setenv("HOMEBOX_FIRST_INDEX", strconv.Itoa(index))
	binary := filepath.Join(dir, "box")

	run := func(timeout time.Duration, executable string, args ...string) (string, error) {
		t.Helper()
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, args...)
		if executable == "go" {
			cmd.Dir = ".."
		}
		cmd.WaitDelay = 5 * time.Second
		output, err := cmd.CombinedOutput()
		t.Logf("%s %s took %s", filepath.Base(executable), args[0], time.Since(start).Round(time.Millisecond))
		return string(output), errors.Join(err, ctx.Err())
	}
	box := func(timeout time.Duration, args ...string) string {
		t.Helper()
		output, err := run(timeout, binary, args...)
		if err != nil {
			t.Fatalf("box %v: %v\n%s", args, err, output)
		}
		return output
	}
	if output, err := run(2*time.Minute, "go", "build", "-o", binary, "./cmd/box"); err != nil {
		t.Fatalf("build box: %v\n%s", err, output)
	}
	laptopKey := filepath.Join(dir, "laptop-key")
	if output, err := run(time.Minute, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", laptopKey); err != nil {
		t.Fatalf("create test laptop key: %v\n%s", err, output)
	}
	box(time.Minute, "host", "init", "--authorized-key", laptopKey+".pub")
	store := &state.Store{Root: root}
	t.Cleanup(func() {
		if _, err := store.Load(name); !errors.Is(err, os.ErrNotExist) {
			if output, err := run(2*time.Minute, binary, "rm", "-f", name); err != nil {
				cleanupOK = false
				t.Errorf("clean up VM (state kept at %s): %v\n%s", root, err, output)
			}
		}
	})

	box(12*time.Minute, "new", name, "--cpus", "2", "--memory", "4", "--disk", "20")
	record, err := store.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if record.Index != index {
		t.Fatalf("box index = %d, want %d", record.Index, index)
	}
	if !record.Provisioned {
		t.Fatal("box new did not mark the VM provisioned")
	}
	assertRunning(t, box(time.Minute, "ls"), name)
	output := box(time.Minute, "ssh", name, "--", "echo ok && whoami")
	if strings.TrimSpace(output) != "ok\n"+record.User {
		t.Fatalf("unexpected SSH command output: %q", output)
	}
	runOK := func(executable string, args ...string) string {
		t.Helper()
		output, err := run(time.Minute, executable, args...)
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", executable, args, err, output)
		}
		return output
	}
	localConfig := store.SSHConfigPath()
	runOK("ssh", "-F", localConfig, name, "true")
	source := filepath.Join(dir, "transfer")
	if err := os.WriteFile(source, []byte("transfer verified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runOK("scp", "-F", localConfig, source, name+":scp-test")
	runOK("rsync", "-e", "ssh -F "+localConfig, source, name+":rsync-test")
	if output := runOK("ssh", "-F", localConfig, name, "cmp scp-test rsync-test && cat scp-test"); output != "transfer verified\n" {
		t.Fatalf("transferred contents = %q", output)
	}
	diagnostics := box(2*time.Minute, "doctor", name)
	t.Log(diagnostics)
	for _, check := range []string{"public-tcp", "public-udp-dns", "public-udp-ntp", "public-icmp", "deny-tcp-", "deny-udp-", "deny-icmp-"} {
		if !strings.Contains(diagnostics, "PASS "+check) {
			t.Fatalf("doctor did not pass %s", check)
		}
	}
	box(time.Minute, "ssh", name, "--", `set -eu
curl -4 --noproxy '*' -fsS -m 8 -o /dev/null https://1.1.1.1
if curl -6 --noproxy '*' -fsS -m 3 -o /dev/null 'https://[2606:4700:4700::1111]'; then
    echo 'unexpected IPv6 egress' >&2
    exit 1
fi
(
    sudo ip route add 1.1.1.1/32 via 192.168.5.2 dev eth0
    trap 'sudo ip route del 1.1.1.1/32 via 192.168.5.2 dev eth0' EXIT
    if nc -z -w 2 1.1.1.1 443 ||
        dig +time=1 +tries=1 +short @1.1.1.1 example.com A | grep -Eq '^[0-9]+\.' ||
        ping -4 -c1 -W1 1.1.1.1; then
        echo 'public egress escaped through the built-in NIC' >&2
        exit 1
    fi
)
`)
	targets, checks, closeCanaries := (doctor.Host{}).Probe(t.Context(), "192.168.5.2", record.SSHPort, false)
	var controls strings.Builder
	if !doctor.Write(&controls, checks) {
		closeCanaries()
		t.Fatalf("Docker isolation controls failed:\n%s", controls.String())
	}
	defer closeCanaries()
	box(2*time.Minute, "ssh", name, "--", `set -eu
docker run -i --rm python:3.14-alpine python - `+strings.Join(targets, " ")+` 10.114.0.1:53 <<'PYTHON'
import socket
import subprocess
import sys
import urllib.request

with urllib.request.urlopen("https://example.com", timeout=8) as response:
    assert response.status == 200
subprocess.run(["nslookup", "example.com", "9.9.9.9"], timeout=8, check=True)
for target in sys.argv[1:]:
    host, port = target.split(":")
    for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
        with socket.socket(socket.AF_INET, kind) as conn:
            conn.settimeout(1)
            try:
                conn.connect((host, int(port)))
                if kind == socket.SOCK_DGRAM:
                    conn.send(b"homebox doctor")
                    conn.recv(128)
            except OSError:
                continue
            raise AssertionError(f"container reached {target} via {kind.name}")
PYTHON
`)
	closeCanaries()

	box(time.Minute, "ssh", name, "--", `set -e
dir=$(mktemp -d)
printf 'homebox e2e\n' >"$dir/index.html"
nohup python3 -m http.server 8000 --bind 0.0.0.0 --directory "$dir" </dev/null >"$dir/server.log" 2>&1 &`)
	if len(record.Forwards) == 0 || record.Forwards[0].GuestPort != 8000 {
		t.Fatal("missing first HTTP forward")
	}
	url := strings.TrimSpace(box(time.Minute, "open", name, "--print"))
	if want := fmt.Sprintf("http://127.0.0.1:%d/", record.Forwards[0].HostPort); url != want {
		t.Fatalf("box open --print = %q, want %q", url, want)
	}
	checkHTTP(t, url)
	if output := runOK("curl", "--noproxy", "*", "--fail", "--silent", "--show-error", url); output != "homebox e2e\n" {
		t.Fatalf("curl response = %q", output)
	}

	config := box(time.Minute, "ssh-config", "--host", "example.local")
	for _, want := range []string{"Host " + name, "HostName example.local", fmt.Sprintf("Port %d", record.SSHPort), "User " + record.User} {
		if !strings.Contains(config, want+"\n") {
			t.Fatalf("ssh-config is missing %q:\n%s", want, config)
		}
	}
	laptopHome := filepath.Join(dir, "laptop")
	laptopSSH := filepath.Join(laptopHome, ".ssh")
	installer := box(time.Minute, "ssh-config", "--installer", "--host", "127.0.0.1")
	scriptPath := filepath.Join(dir, "homebox-ssh.sh")
	if err := os.WriteFile(scriptPath, []byte(installer), 0600); err != nil {
		t.Fatal(err)
	}
	runOK("env", "HOME="+laptopHome, "sh", scriptPath)
	laptopConfig := filepath.Join(laptopSSH, "config")
	laptopPins := filepath.Join(laptopSSH, "homebox_known_hosts")
	pins, err := os.ReadFile(laptopPins)
	if err != nil || string(pins) != box(time.Minute, "ssh-config", "--known-hosts") {
		t.Fatalf("installed laptop pins differ from the export: %v", err)
	}
	// OpenSSH expands ~ using the account's home, regardless of HOME.
	// Point the test client at the sandbox copies of the installed files.
	for _, path := range []string{laptopConfig, filepath.Join(laptopSSH, "homebox_config")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.ReplaceAll(string(data), "~/.ssh/", laptopSSH+"/"))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	laptopArgs := []string{"-F", laptopConfig, "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-i", laptopKey, name, "true"}
	runOK("ssh", laptopArgs...)
	// The test laptop's public key is a valid key that cannot match the VM's key.
	wrongKey, err := os.ReadFile(laptopKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(laptopPins, append([]byte("homebox-"+name+" "), wrongKey...), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := run(time.Minute, "ssh", laptopArgs...); err == nil || !strings.Contains(output, "REMOTE HOST IDENTIFICATION HAS CHANGED") {
		t.Fatalf("tampered pins did not trigger a host-key mismatch: %v\n%s", err, output)
	}
	t.Log("SSH rejected the tampered host key")
	box(2*time.Minute, "stop", name)
	box(3*time.Minute, "start", name)
	t.Log(box(2*time.Minute, "doctor", name))
	box(2*time.Minute, "rm", "-f", name)
	if output := box(time.Minute, "ls"); strings.Contains(output, name) {
		t.Fatalf("removed VM still in listing:\n%s", output)
	}
}

func assertRunning(t *testing.T, listing, name string) {
	t.Helper()
	for line := range strings.SplitSeq(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == name && fields[1] == "running" {
			return
		}
	}
	t.Fatalf("%s is missing or stopped:\n%s", name, listing)
}

func checkHTTP(t *testing.T, url string) {
	t.Helper()
	start := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	var lastResult string
	for {
		resp, err := client.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK && string(body) == "homebox e2e\n" {
				t.Logf("HTTP forwarding took %s", time.Since(start).Round(time.Millisecond))
				return
			}
			lastResult = fmt.Sprintf("status %s, body %q, read error %v", resp.Status, body, readErr)
		} else {
			lastResult = err.Error()
		}
		select {
		case <-ctx.Done():
			t.Fatalf("HTTP forward did not serve the guest response: %s", lastResult)
		case <-time.After(250 * time.Millisecond):
		}
	}
}
