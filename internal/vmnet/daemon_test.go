//go:build darwin || linux

package vmnet

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "_vmnet" {
		config, err := ParseArgs(os.Args[2:])
		if err == nil {
			err = Run(context.Background(), config)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRelayFollowsHostProcess(t *testing.T) {
	testRelayHostProcess(t, true)
}

func TestRelayRefusesWrongListenerOwner(t *testing.T) {
	testRelayHostProcess(t, false)
}

func testRelayHostProcess(t *testing.T, ownsTarget bool) {
	t.Helper()
	config, target := testConfig(t)
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	var runErr error
	go func() {
		defer close(stopped)
		runErr = Run(ctx, config)
	}()
	defer func() {
		cancel()
		<-stopped
		if runErr != nil {
			t.Error(runErr)
		}
	}()
	waitReady(t, config, stopped)
	assertRelayClosed(t, config)
	command := exec.Command("/bin/sleep", "30")
	if ownsTarget {
		listenerFile, err := target.File()
		if err != nil {
			t.Fatal(err)
		}
		defer listenerFile.Close()
		command.ExtraFiles = []*os.File{listenerFile}
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Wait()
	defer command.Process.Kill()
	if err := os.WriteFile(config.HostPIDFile, []byte(strconv.Itoa(command.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	if ownsTarget {
		go func() {
			client, err := target.Accept()
			if err == nil {
				defer client.Close()
				io.WriteString(client, "SSH-2.0-test\r\n")
			}
		}()
	}
	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.DialTimeout("tcp4", endpoint(config.SSHPublish.IP, config.SSHPublish.Port), 25*time.Millisecond)
		if err == nil {
			conn.SetReadDeadline(time.Now().Add(time.Second))
			banner, err := io.ReadAll(conn)
			conn.Close()
			if ownsTarget && (err != nil || string(banner) != "SSH-2.0-test\r\n") {
				t.Fatalf("SSH banner %q: %v", banner, err)
			}
			if !ownsTarget {
				if len(banner) != 0 {
					t.Fatalf("unexpected SSH banner %q", banner)
				}
				target.SetDeadline(time.Now().Add(50 * time.Millisecond))
				if upstream, err := target.AcceptTCP(); err == nil {
					upstream.Close()
					t.Fatal("relay dialed a listener owned by another process")
				} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
					t.Fatal(err)
				}
				log, err := os.ReadFile(config.LogFile)
				if err != nil || !strings.Contains(string(log), "SSH relay refused unverified target owner") {
					t.Fatalf("missing refusal warning: %v", err)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SSH relay did not start")
		}
		time.Sleep(pollInterval)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("daemon did not close the relay after host process exit")
	}
	assertRelayClosed(t, config)
}

func assertRelayClosed(t *testing.T, config Config) {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", endpoint(config.SSHPublish.IP, config.SSHPublish.Port), 50*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("relay listener is open without a live host process")
	}
}

func TestSpawnAndStop(t *testing.T) {
	config, _ := testConfig(t)
	if err := os.WriteFile(config.PIDFile, []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Running(config.PIDFile); ok {
		t.Fatal("an unlocked PID file must not identify a live daemon")
	}
	if _, err := os.Stat(config.PIDFile); !os.IsNotExist(err) {
		t.Fatalf("stale PID file was not removed: %v", err)
	}
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: config.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := Spawn(exe, []string{"_vmnet"}, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(config.PIDFile) })
	if ready, ok := Running(config.PIDFile); !ok || ready != pid {
		t.Fatalf("ready PID %d, want %d", ready, pid)
	}
	if _, err := Spawn(exe, []string{"_vmnet"}, config); err == nil {
		t.Fatal("second daemon started")
	}
	if err := Stop(config.PIDFile); err != nil {
		t.Fatal(err)
	}
	if _, ok := Running(config.PIDFile); ok {
		t.Fatal("daemon still running after Stop")
	}
}

func TestSpawnRefusesNonSocketPath(t *testing.T) {
	config, _ := testConfig(t)
	if err := os.WriteFile(config.Socket, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(exe, []string{"_vmnet"}, config); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("expected socket path failure, got %v", err)
	}
	if _, ok := Running(config.PIDFile); ok {
		t.Fatal("failed daemon published readiness")
	}
	if data, err := os.ReadFile(config.Socket); err != nil || string(data) != "preserve" {
		t.Fatalf("path contents %q: %v", data, err)
	}
}

func testConfig(t *testing.T) (Config, *net.TCPListener) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hb-net-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	target, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close() })
	published, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := published.Addr().(*net.TCPAddr).Port
	published.Close()
	return Config{
		Name: "test", Socket: filepath.Join(dir, "net.sock"),
		HostPIDFile: filepath.Join(dir, "host.pid"), SSHPublish: Endpoint{"127.0.0.1", port},
		SSHTarget: target.Addr().(*net.TCPAddr).Port,
		PIDFile:   filepath.Join(dir, "vmnet.pid"), LogFile: filepath.Join(dir, "vmnet.log"),
	}, target
}

func waitReady(t *testing.T, config Config, stopped <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if _, ok := Running(config.PIDFile); ok {
			return
		}
		select {
		case <-stopped:
			t.Fatal("daemon stopped before readiness")
		case <-timer.C:
			t.Fatal("daemon did not become ready")
		case <-ticker.C:
		}
	}
}
