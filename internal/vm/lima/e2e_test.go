package lima

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

var ubuntuImages = []vm.Image{
	{Arch: "aarch64", URL: "https://cloud-images.ubuntu.com/releases/resolute/release-20261016/ubuntu-26.04-server-cloudimg-arm64.img", Digest: "sha256:8eb876c2ccc4b6ff89e4a65d511815923b285e04503ba465809bf587f763da24"},
	{Arch: "x86_64", URL: "https://cloud-images.ubuntu.com/releases/resolute/release-20261016/ubuntu-26.04-server-cloudimg-amd64.img", Digest: "sha256:5645d0318ad21f643ebb1ac4567514620e9893380b07b0201fda163baa5b7eb5"},
}

func TestLimaLifecycle(t *testing.T) {
	if os.Getenv("HOMEBOX_E2E") != "1" {
		t.Skip("set HOMEBOX_E2E=1 to boot a Lima VM")
	}
	// Lima's Unix socket paths must fit inside sockaddr_un.
	home, err := os.MkdirTemp("/tmp", "homebox-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	driver := New(Options{Home: home})
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	const name = "test-lima"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := driver.Delete(cleanup, name); err != nil {
			t.Error(err)
		}
	})

	sshPort, forwardPort, hiddenPort, replacementPort, guestPort := freePorts(t)
	spec := vm.Spec{
		Name: name, User: "box", Images: ubuntuImages, CPUs: 2, MemoryGiB: 2, DiskGiB: 10,
		SSHPort:  sshPort,
		Forwards: []vm.PortForward{{GuestPort: guestPort, HostPort: forwardPort, HostIP: "127.0.0.1"}},
	}
	run := func(label string, operation func() error) {
		t.Helper()
		start := time.Now()
		if err := operation(); err != nil {
			t.Fatalf("%s after %s: %v", label, time.Since(start).Round(time.Millisecond), err)
		}
		t.Logf("%s: %s", label, time.Since(start).Round(time.Millisecond))
	}
	start := func() error { return driver.Start(ctx, name, vm.StartOptions{}) }
	stop := func() error { return driver.Stop(ctx, name) }

	run("create", func() error { return driver.Create(ctx, spec) })
	run("first start", start)
	if err := driver.SetForwards(ctx, name, nil); err == nil {
		t.Fatal("edited forwards while running")
	}

	checkSSH := func() vm.Endpoint {
		t.Helper()
		endpoint, err := driver.SSH(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		guestCommand(t, ctx, endpoint, "true")
		guestCommand(t, ctx, endpoint, "test \"$HOME\" = /home/box && test -z \"$SSH_AUTH_SOCK\"")
		return endpoint
	}
	serve := func(endpoint vm.Endpoint, guestPort int) {
		t.Helper()
		command := fmt.Sprintf("nohup python3 -m http.server %[1]d --bind 127.0.0.1 </dev/null >/tmp/homebox-%[1]d.log 2>&1 &", guestPort)
		guestCommand(t, ctx, endpoint, command)
		guestCommand(t, ctx, endpoint, fmt.Sprintf("curl -fsS --retry 5 --retry-connrefused http://127.0.0.1:%d/ >/dev/null", guestPort))
	}

	endpoint := checkSSH()
	checkHostLoopback(t, ctx, endpoint)
	serve(endpoint, guestPort)
	serve(endpoint, hiddenPort)
	checkForward(t, forwardPort, true)
	checkForward(t, hiddenPort, false)
	checkForward(t, guestPort, false)

	run("stop", stop)
	run("edit stopped forwards", func() error {
		return driver.SetForwards(ctx, name, []vm.PortForward{{GuestPort: guestPort, HostPort: replacementPort, HostIP: "127.0.0.1"}})
	})
	run("restart", start)
	serve(checkSSH(), guestPort)
	checkForward(t, replacementPort, true)
	checkForward(t, forwardPort, false)
	run("second stop", stop)
	run("delete", func() error { return driver.Delete(ctx, name) })
	if _, err := driver.Get(ctx, name); !errors.Is(err, vm.ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}

func freePorts(t *testing.T) (int, int, int, int, int) {
	t.Helper()
	var ports [5]int
	for index := range ports {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		ports[index] = listener.Addr().(*net.TCPAddr).Port
	}
	return ports[0], ports[1], ports[2], ports[3], ports[4]
}

func guestCommand(t *testing.T, ctx context.Context, endpoint vm.Endpoint, command string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "ssh", "-F", "/dev/null",
		"-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile="+endpoint.KnownHosts, "-i", endpoint.IdentityFile,
		"-p", strconv.Itoa(endpoint.Port), endpoint.User+"@"+endpoint.Host, command)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("guest command %q: %v\n%s", command, err, output)
	}
}

func checkForward(t *testing.T, port int, reachable bool) {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if !reachable {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			t.Fatalf("unexpected listener on %s", addr)
		}
		return
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HTTP through %s: %s", addr, response.Status)
	}
}

func checkHostLoopback(t *testing.T, ctx context.Context, endpoint vm.Endpoint) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	defer server.Close()
	go server.Serve(listener)
	port := listener.Addr().(*net.TCPAddr).Port
	checkForward(t, port, true)
	// The literal gateway address exercises the loopback guard independently of DNS.
	command := fmt.Sprintf("curl --noproxy '*' -sS --connect-timeout 2 --max-time 3 http://192.168.5.2:%d/ >/dev/null; case $? in 7|28) exit 0;; *) exit 1;; esac", port)
	guestCommand(t, ctx, endpoint, command)
}
