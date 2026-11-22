//go:build darwin || linux

package vmnet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func TestRun(t *testing.T) {
	config, target := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	upstream, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		buffer := make([]byte, 512)
		size, addr, err := upstream.ReadFromUDP(buffer)
		if err == nil && size >= 12 {
			buffer[2] = 0x80
			upstream.WriteToUDP(buffer[:size], addr)
		}
	}()
	target.SetDeadline(time.Now().Add(5 * time.Second))
	go func() {
		client, err := target.Accept()
		if err == nil {
			defer client.Close()
			io.WriteString(client, "SSH-2.0-test\r\n")
		}
	}()

	stopped := make(chan struct{})
	var runErr error
	go func() {
		defer close(stopped)
		runErr = run(ctx, config, []string{upstream.LocalAddr().String()})
	}()
	defer func() {
		cancel()
		<-stopped
		if runErr != nil {
			t.Error(runErr)
		}
		if _, ok := Running(config.PIDFile); ok {
			t.Error("daemon still running")
		}
	}()
	waitReady(t, config, stopped)
	checkProxy(t, config)
	checkRelay(t, config)

	dns, err := net.DialTimeout("udp4", endpoint("127.0.0.1", config.DNSPort), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer dns.Close()
	dns.SetDeadline(time.Now().Add(2 * time.Second))
	query := []byte{12, 34, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := dns.Write(query); err != nil {
		t.Fatal(err)
	}
	query[2] = 0x80
	answer := make([]byte, 512)
	size, err := dns.Read(answer)
	if err != nil || !bytes.Equal(answer[:size], query) {
		t.Fatalf("DNS answer %x: %v", answer[:size], err)
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
	checkProxy(t, config)
	if err := Stop(config.PIDFile); err != nil {
		t.Fatal(err)
	}
	if _, ok := Running(config.PIDFile); ok {
		t.Fatal("daemon still running after Stop")
	}
}

func TestSpawnBindFailure(t *testing.T) {
	config, _ := testConfig(t)
	file, err := os.Create(config.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(11 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: config.DNSPort})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(exe, []string{"_vmnet"}, config); err == nil || !strings.Contains(err.Error(), "bind DNS") {
		t.Fatalf("expected DNS bind failure, got %v", err)
	}
	if _, ok := Running(config.PIDFile); ok {
		t.Fatal("failed daemon published readiness")
	}
	proxy, err := net.Listen("tcp4", endpoint("127.0.0.1", config.ProxyPort))
	if err != nil {
		t.Fatalf("proxy socket leaked: %v", err)
	}
	proxy.Close()
}

func testConfig(t *testing.T) (Config, *net.TCPListener) {
	t.Helper()
	dir := t.TempDir()
	listeners := make([]*net.TCPListener, 4)
	ports := make([]int, len(listeners))
	for index := range listeners {
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		listeners[index] = listener
		ports[index] = listener.Addr().(*net.TCPAddr).Port
	}
	for _, listener := range listeners[:3] {
		listener.Close()
	}
	return Config{
		Name: "test", ProxyPort: ports[0], DNSPort: ports[1], SSHPublish: Endpoint{"127.0.0.1", ports[2]},
		SSHTarget: ports[3], PIDFile: filepath.Join(dir, "vmnet.pid"), LogFile: filepath.Join(dir, "vmnet log.json"),
	}, listeners[3]
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

func checkProxy(t *testing.T, config Config) {
	t.Helper()
	proxy := &url.URL{Scheme: "http", Host: endpoint("127.0.0.1", config.ProxyPort)}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("proxy status %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}

func checkRelay(t *testing.T, config Config) {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", endpoint(config.SSHPublish.IP, config.SSHPublish.Port), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if banner, err := io.ReadAll(conn); err != nil || string(banner) != "SSH-2.0-test\r\n" {
		t.Fatalf("SSH banner %q: %v", banner, err)
	}
}
