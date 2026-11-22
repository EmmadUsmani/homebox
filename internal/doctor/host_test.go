package doctor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

func TestHostProbeAndCleanup(t *testing.T) {
	ssh, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ssh.Close()
	go func() {
		conn, err := ssh.Accept()
		if err == nil {
			defer conn.Close()
			io.WriteString(conn, "SSH-2.0-test\r\n")
		}
	}()
	const guestHost = "192.0.2.10"
	host := Host{}
	targets, checks, closeCanaries := host.probe(t.Context(), guestHost, ssh.Addr().(*net.TCPAddr).Port, false, nil)
	defer closeCanaries()
	var output bytes.Buffer
	if !Write(&output, checks) || len(targets) != 1 {
		t.Fatalf("targets %v, checks:\n%s", targets, &output)
	}
	ip, port, err := net.SplitHostPort(targets[0])
	if err != nil || ip != guestHost {
		t.Fatalf("loopback target = %q, %v", targets[0], err)
	}
	localCanary := net.JoinHostPort("127.0.0.1", port)
	conn, err := net.DialTimeout("tcp4", localCanary, probeTimeout)
	if err != nil {
		t.Fatalf("control was closed before guest probe: %v", err)
	}
	conn.Close()

	closeCanaries()
	if conn, err := net.DialTimeout("tcp4", localCanary, probeTimeout); err == nil {
		conn.Close()
		t.Fatal("canary still listening")
	}
}

func TestPublishedSSH(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().(*net.TCPAddr)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(probeTimeout))
			io.WriteString(conn, "HTTP/1.1 200 OK\r\n")
			conn.Close()
		}
	}()
	if check := publishedSSH(t.Context(), address.IP.String(), address.Port, true); check.OK {
		t.Fatal("HTTP banner passed SSH check")
	}
	if check := publishedSSH(t.Context(), address.IP.String(), address.Port, false); check.OK {
		t.Fatal("open TCP port passed disabled LAN check")
	}
	listener.Close()
	<-done
	if check := publishedSSH(t.Context(), address.IP.String(), address.Port, false); !check.OK {
		t.Fatalf("closed TCP port failed disabled LAN check: %+v", check)
	}
}

func TestMacGatewayAndRouterControl(t *testing.T) {
	const gateway = "192.0.2.1"
	command := "route -n get default"
	runner := &execx.Fake{}
	runner.On(command, "   route to: default\n gateway: "+gateway+"\ninterface: en0\n", nil)
	var attempts []string
	host := Host{
		Gateway: func(ctx context.Context) (netip.Addr, error) { return macGateway(ctx, runner) },
		dialRouter: func(_ context.Context, network, address string) (net.Conn, error) {
			attempts = append(attempts, network+" "+address)
			if address != gateway+":443" {
				return nil, errors.New("connection refused")
			}
			local, peer := net.Pipe()
			peer.Close()
			return local, nil
		},
	}
	target, check := host.router(t.Context())
	if !check.OK || check.Skipped || target != gateway+":443" {
		t.Fatalf("router = %q, %+v", target, check)
	}
	if !slices.Equal(runner.Commands(), []string{command}) ||
		!slices.Equal(attempts, []string{"tcp4 " + gateway + ":80", "tcp4 " + gateway + ":443"}) {
		t.Fatalf("commands %v, connections %v", runner.Commands(), attempts)
	}
	host.dialRouter = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unreachable") }
	if target, check := host.router(t.Context()); target != gateway+":80" || !check.Skipped || check.OK {
		t.Fatalf("unreachable router = %q, %+v", target, check)
	}
}

func TestInterfaceIPv4(t *testing.T) {
	var addresses []net.Addr
	for _, value := range []string{"192.0.2.3/24", "127.0.0.1/8", "2001:db8::2/64", "192.0.2.2/24", "192.0.2.3/24"} {
		ip, network, err := net.ParseCIDR(value)
		if err != nil {
			t.Fatal(err)
		}
		network.IP = ip
		addresses = append(addresses, network)
	}
	if got := interfaceIPv4(addresses); !slices.Equal(got, []string{"192.0.2.2", "192.0.2.3"}) {
		t.Fatalf("interface IPv4 addresses = %v", got)
	}
}

func TestWrite(t *testing.T) {
	var out bytes.Buffer
	if Write(&out, []Check{
		{Name: "control", OK: true},
		{Name: "router", Skipped: true, Detail: "unreachable"},
		{Name: "ssh", Detail: "no banner"},
	}) {
		t.Fatal("reported success despite failure")
	}
	if want := "PASS control\nSKIP router: unreachable\nFAIL ssh: no banner\n"; out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}
