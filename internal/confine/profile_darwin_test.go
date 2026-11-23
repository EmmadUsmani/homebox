package confine

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSeatbeltHostAgentResolvesCacheDirectory(t *testing.T) {
	root := t.TempDir()
	instance := filepath.Join(root, `quotes " and slash \`)
	if err := os.Mkdir(instance, 0700); err != nil {
		t.Fatal(err)
	}
	wrap, err := Prepare(Rules{
		RuntimeDir: instance, Home: root, LimaHome: root, Executable: "/usr/bin/getconf",
		ConnectTCP: []int{32201},
		ConnectUDP: []int{23101},
		BindTCP:    []Bind{{Port: 32201}, {Port: 10100, LAN: true}},
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	sandbox(t, wrap, "/usr/bin/getconf", "DARWIN_USER_CACHE_DIR")
}

func TestSeatbeltConnections(t *testing.T) {
	listen := func() *net.TCPListener {
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		return listener
	}
	allowed, denied := listen(), listen()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := Prepare(Rules{
		RuntimeDir: dir, Home: dir, LimaHome: dir, Executable: exe,
		ConnectTCP: []int{allowed.Addr().(*net.TCPAddr).Port},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	sandbox(t, wrap, exe, "-test.run=^TestSeatbeltDialProbe$", "--", allowed.Addr().String(), denied.Addr().String())
}

func sandbox(t *testing.T, wrap []string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, wrap[0], append(wrap[1:], args...)...)
	command.Dir = "/"
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sandbox-exec: %v\n%s", err, output)
	}
}

func TestSeatbeltDialProbe(t *testing.T) {
	if len(os.Args) != 5 || os.Args[2] != "--" {
		return
	}
	conn, err := net.DialTimeout("tcp4", os.Args[3], time.Second)
	if err != nil {
		t.Fatalf("allowed dial: %v", err)
	}
	conn.Close()
	for _, address := range []string{os.Args[4], "1.1.1.1:443"} {
		conn, err := net.DialTimeout("tcp4", address, time.Second)
		if conn != nil {
			conn.Close()
		}
		if !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) {
			t.Errorf("dial %s: expected sandbox denial, got %v", address, err)
		}
	}
}

func TestSeatbeltDaemonCannotReexec(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := Prepare(Rules{
		Daemon: true, RuntimeDir: dir, Executable: exe,
		BindTCP: []Bind{{Port: 23001}},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	sandbox(t, wrap, exe, "-test.run=^TestSeatbeltDaemonReexecProbe$", "--", exe)
}

func TestSeatbeltDaemonReexecProbe(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	exe := os.Args[3]
	err := syscall.Exec(exe, []string{exe, "-unexpected-flag"}, os.Environ())
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("re-exec: expected sandbox denial, got %v", err)
	}
}

func TestSeatbeltDaemonAllowsOnlyIPv4Outbound(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hb-sandbox-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	addresses := []string{"127.0.0.1:0", "[::1]:0", filepath.Join(dir, "other.sock")}
	for i, network := range []string{"tcp4", "tcp6", "unix"} {
		listener, err := net.Listen(network, addresses[i])
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		addresses[i] = listener.Addr().String()
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := Prepare(Rules{
		Daemon: true, RuntimeDir: dir, Executable: exe,
		GatewaySocket: filepath.Join(dir, "gateway.sock"), BindTCP: []Bind{{Port: 2201}},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{exe, "-test.run=^TestSeatbeltDaemonNetworkProbe$", "--"}, addresses...)
	sandbox(t, wrap, args...)
}

func TestSeatbeltDaemonNetworkProbe(t *testing.T) {
	if len(os.Args) != 6 || os.Args[2] != "--" {
		return
	}
	for i, network := range []string{"tcp4", "tcp6", "unix"} {
		conn, err := net.DialTimeout(network, os.Args[i+3], time.Second)
		if conn != nil {
			conn.Close()
		}
		switch {
		case i == 0 && err != nil:
			t.Errorf("IPv4 dial failed: %v", err)
		case i != 0 && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES):
			t.Errorf("%s dial: expected sandbox denial, got %v", network, err)
		}
	}
}
