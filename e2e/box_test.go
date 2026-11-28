// Package e2e exercises the box CLI against a real VM.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	cleanupVM := func(name string) {
		t.Cleanup(func() {
			if _, err := store.Load(name); !errors.Is(err, os.ErrNotExist) {
				if output, err := run(2*time.Minute, binary, "rm", "-f", name); err != nil {
					cleanupOK = false
					t.Errorf("clean up VM (state kept at %s): %v\n%s", root, err, output)
				}
			}
		})
	}
	cleanupVM(name)

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
	checkLogs(t, binary, name, box)
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
	// A plain `ssh host cmd` must find the tools, not only a login shell.
	runOK("ssh", "-F", localConfig, name, "node --version && claude --version && codex --version")
	source := filepath.Join(dir, "transfer")
	if err := os.WriteFile(source, []byte("transfer verified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runOK("scp", "-F", localConfig, source, name+":scp-test")
	runOK("rsync", "-e", "ssh -F "+localConfig, source, name+":rsync-test")
	if output := runOK("ssh", "-F", localConfig, name, "cmp scp-test rsync-test && cat scp-test"); output != "transfer verified\n" {
		t.Fatalf("transferred contents = %q", output)
	}
	checkRelayDropsDeadClients(t, localConfig, name, func() string {
		return box(time.Minute, "ssh", name, "--", `pgrep -fc "sshd-session: .*\[priv\]" || true`)
	})
	output, stressErr := run(time.Minute, binary, "ssh", name, "--", egressStress)
	t.Log(output)
	daemonLog, err := os.ReadFile(filepath.Join(root, "boxes", name, "run", "vmnet.log"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(daemonLog), "\n") {
		if strings.Contains(line, "egress flow refused") {
			t.Fatalf("gateway refused flows during provisioning or stress test: %s", line)
		}
	}
	if stressErr != nil {
		t.Fatalf("egress stress: %v", stressErr)
	}
	diagnostics := box(2*time.Minute, "doctor", name)
	t.Log(diagnostics)
	for _, check := range []string{"ssh-relay-target-owner", "public-tcp", "public-udp-dns", "public-udp-ntp", "public-icmp", "deny-tcp-", "deny-udp-", "deny-icmp-"} {
		if !strings.Contains(diagnostics, "PASS "+check) {
			t.Fatalf("doctor did not pass %s", check)
		}
	}
	if runtime.GOOS == "linux" {
		for _, check := range []string{"sandbox-hostagent", "sandbox-qemu", "sandbox-vmnet", "slirp-deny-tcp-", "slirp-deny-udp-", "slirp-deny-icmp-"} {
			if !strings.Contains(diagnostics, "PASS "+check) {
				t.Fatalf("doctor did not pass %s", check)
			}
		}
	} else {
		for _, process := range []string{"hostagent", "vmnet"} {
			for _, probe := range []string{"home-read", "home-write", "volumes-read", "shared-read", "app-temp-read", "shared-temp-read", "mach-lookup"} {
				if check := process + "-" + probe; !strings.Contains(diagnostics, "PASS "+check) {
					t.Fatalf("doctor did not pass %s", check)
				}
			}
		}
	}
	box(time.Minute, "ssh", name, "--", `set -eu
curl -4 --noproxy '*' -fsS -m 8 -o /dev/null https://1.1.1.1
if curl -6 --noproxy '*' -fsS -m 3 -o /dev/null 'https://[2606:4700:4700::1111]'; then
    echo 'unexpected IPv6 egress' >&2
    exit 1
fi
(
    builtin=$(ip -4 -o route get 192.168.5.2 | awk '{for (i=1; i<NF; i++) if ($i == "dev") print $(i+1)}')
    test -n "$builtin"
    sudo ip route add 1.1.1.1/32 via 192.168.5.2 dev "$builtin"
    trap 'sudo ip route del 1.1.1.1/32 via 192.168.5.2 dev "$builtin"' EXIT
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
	checkStatus(t, box, binary, name, url)
	if output := runOK("curl", "--noproxy", "*", "--fail", "--silent", "--show-error", url); output != "homebox e2e\n" {
		t.Fatalf("curl response = %q", output)
	}

	checkUnpublishedPort(t, name, box)

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
	checkResize(t, name, binary, box, run)
	t.Log(box(2*time.Minute, "doctor", name))
	if runtime.GOOS == "darwin" {
		checkImage(t, name, binary, store, box, run, cleanupVM)
	}
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

func checkUnpublishedPort(t *testing.T, name string, box func(time.Duration, ...string) string) {
	t.Helper()
	candidate, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := candidate.Addr().(*net.TCPAddr).Port
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}

	const marker = "homebox unpublished"
	box(time.Minute, "ssh", name, "--", fmt.Sprintf(`set -eu
dir=$(mktemp -d)
printf '%s\n' > "$dir/index.html"
nohup timeout 40s python3 -m http.server %d --bind 0.0.0.0 --directory "$dir" >"$dir/server.log" 2>&1 </dev/null &
curl -q --noproxy '*' -fsS --retry 5 --retry-connrefused --retry-delay 1 --max-time 2 http://127.0.0.1:%d/ | grep -Fx '%s'
`, marker, port, port, marker))
	// The short-lived fixture belongs to the disposable VM. Sample long
	// enough for background listener discovery to observe a working server.
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		conn, err := net.DialTimeout("tcp4", address, 250*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Fatalf("host opened a listener for unpublished guest port %d", port)
		}
		time.Sleep(250 * time.Millisecond)
	}
	box(time.Minute, "ssh", name, "--", fmt.Sprintf("curl -q --noproxy '*' -fsS --max-time 2 http://127.0.0.1:%d/ | grep -Fx '%s'", port, marker))
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

// Unique subdomains avoid exact-name cache hits. Either a positive or a
// negative answer verifies the UDP round trip.
const egressStress = `set -eu
python3 - <<'PYTHON' &
import asyncio
import socket

async def main():
    loop = asyncio.get_running_loop()
    addresses = []
    hosts = ("www.cloudflare.com", "www.google.com", "www.apple.com",
             "www.microsoft.com", "www.github.com", "www.wikipedia.org")
    for host in hosts:
        records = await loop.getaddrinfo(host, 443, family=socket.AF_INET, type=socket.SOCK_STREAM)
        addresses.append(records[0][4])

    connections = []
    async def connect(address):
        conn = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        connections.append(conn)
        conn.setblocking(False)
        await asyncio.wait_for(loop.sock_connect(conn, address), timeout=10)

    try:
        # Stagger SYNs and keep every socket open until all 300 connect. The
        # gateway refuses a flow at connect time, so a completed connect is the
        # proof; servers may close idle pre-TLS sockets later, which is fine.
        # Rotate across independent public endpoints to keep the load on any one low.
        for start in range(0, 300, 20):
            batch = (addresses[i % len(addresses)] for i in range(start, start + 20))
            await asyncio.gather(*(connect(address) for address in batch))
            await asyncio.sleep(0.05)
    finally:
        for conn in connections:
            conn.close()

asyncio.run(main())
PYTHON
tcp=$!
run=$(date +%s)
seq 1 500 | xargs -P 64 -I '{}' sh -eu -c '
    name="hb-$1-$2.example.com"
    # Quad9 rate-limits a burst like this from one address, even straight
    # from the host, so spread it over resolvers that do not.
    case $(($2 % 4)) in
        0) resolver=1.1.1.1 ;;
        1) resolver=8.8.8.8 ;;
        2) resolver=1.0.0.1 ;;
        3) resolver=8.8.4.4 ;;
    esac
    if result=$(dig -4 "@$resolver" +notcp +ignore +time=2 +tries=2 "$name" A) &&
        printf "%s\n" "$result" | grep -Eq "status: (NOERROR|NXDOMAIN),"; then
        exit 0
    fi
    printf "UDP DNS failed for %s via %s\n%s\n" "$name" "$resolver" "$result" >&2
    exit 1
' sh "$run" '{}' &
dns=$!
status=0
wait "$tcp" || status=1
wait "$dns" || status=1
test "$status" -eq 0
echo "300 concurrent public TCP sockets and 500 distinct UDP DNS replies succeeded"
`
