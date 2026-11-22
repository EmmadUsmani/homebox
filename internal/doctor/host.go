package doctor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

const probeTimeout = time.Second

// Host probes local network services and supplies guest deny targets.
type Host struct {
	Gateway    func(context.Context) (netip.Addr, error)
	dialRouter func(context.Context, string, string) (net.Conn, error)
}

// Probe runs the host controls and keeps the canaries open until closeCanaries is called.
func (h Host) Probe(ctx context.Context, guestHost string, sshPort int, lan bool) (targets []string, checks []Check, closeCanaries func()) {
	addresses, err := net.InterfaceAddrs()
	targets, checks, closeCanaries = h.probe(ctx, guestHost, sshPort, lan, interfaceIPv4(addresses))
	if err != nil {
		checks = append(checks, Check{Name: "lan-interfaces", Detail: err.Error()})
	}
	return
}

func (h Host) probe(ctx context.Context, guestHost string, sshPort int, lan bool, addresses []string) (targets []string, checks []Check, closeCanaries func()) {
	var servers []*http.Server
	closeCanaries = func() {
		for _, server := range servers {
			server.Close()
		}
	}

	loopback := "127.0.0.1"
	for _, bind := range append([]string{loopback}, addresses...) {
		guestAddress := bind
		if bind == loopback {
			guestAddress = guestHost
		}
		target, server, err := canary(ctx, bind, guestAddress)
		check := Check{Name: "canary-" + bind, OK: err == nil}
		if err != nil {
			check.Detail = err.Error()
		} else {
			servers = append(servers, server)
			targets = append(targets, target)
		}
		checks = append(checks, check)
	}

	router, check := h.router(ctx)
	if check.OK {
		targets = append(targets, router)
	}
	checks = append(checks, check, publishedSSH(ctx, loopback, sshPort, true))
	if len(addresses) == 0 {
		checks = append(checks, Check{Name: "ssh-lan", Skipped: true, Detail: "no non-loopback IPv4 addresses"})
	}
	for _, ip := range addresses {
		checks = append(checks, publishedSSH(ctx, ip, sshPort, lan))
	}
	return
}

func interfaceIPv4(addresses []net.Addr) []string {
	var result []string
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() && !prefix.Addr().IsLoopback() && !prefix.Addr().IsUnspecified() {
			result = append(result, prefix.Addr().String())
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func canary(ctx context.Context, bind, guestAddress string) (target string, server *http.Server, err error) {
	if ip, err := netip.ParseAddr(guestAddress); err != nil || !ip.Is4() {
		return "", nil, errors.New("missing or invalid guest-visible host IPv4 address")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", net.JoinHostPort(bind, "0"))
	if err != nil {
		return "", nil, err
	}
	server = &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		ReadHeaderTimeout: probeTimeout,
	}
	go server.Serve(listener)

	transport := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: probeTimeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.Addr().String(), nil)
	if err == nil {
		var response *http.Response
		response, err = client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				err = fmt.Errorf("expected HTTP 200, got %s", response.Status)
			}
		}
	}
	if err != nil {
		listener.Close()
		server.Close()
		return "", nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return net.JoinHostPort(guestAddress, strconv.Itoa(port)), server, nil
}

func (h Host) router(ctx context.Context) (string, Check) {
	check := Check{Name: "router-control", Skipped: true, Detail: "default gateway lookup unavailable"}
	if h.Gateway == nil {
		return "", check
	}
	ip, err := h.Gateway(ctx)
	if err != nil {
		check.Detail = err.Error()
		return "", check
	}
	dial := h.dialRouter
	if dial == nil {
		dial = (&net.Dialer{Timeout: probeTimeout}).DialContext
	}
	for _, port := range []string{"80", "443"} {
		target := net.JoinHostPort(ip.String(), port)
		conn, err := dial(ctx, "tcp4", target)
		if err == nil {
			conn.Close()
			return target, Check{Name: "router-control-" + target, OK: true}
		}
	}
	check.Detail = fmt.Sprintf("host could not reach %s on port 80 or 443", ip)
	return "", check
}

func publishedSSH(ctx context.Context, ip string, port int, wantReachable bool) Check {
	target := net.JoinHostPort(ip, strconv.Itoa(port))
	check := Check{Name: "published-ssh-" + target}
	conn, err := (&net.Dialer{Timeout: probeTimeout}).DialContext(ctx, "tcp4", target)
	if err != nil {
		check.OK = !wantReachable && ctx.Err() == nil
		check.Detail = err.Error()
		return check
	}
	defer conn.Close()
	if !wantReachable {
		check.Detail = "TCP port reachable with LAN publishing off"
		return check
	}
	conn.SetReadDeadline(time.Now().Add(probeTimeout))
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	banner, err := bufio.NewReaderSize(conn, 4096).ReadSlice('\n')
	check.OK = err == nil && strings.HasPrefix(string(banner), "SSH-")
	check.Detail = "missing SSH banner"
	return check
}
