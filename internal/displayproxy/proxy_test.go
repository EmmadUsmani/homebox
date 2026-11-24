package displayproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func testProxy(t *testing.T, handler http.Handler, ttl time.Duration) (*Proxy, context.CancelFunc) {
	t.Helper()
	guest := httptest.NewServer(handler)
	t.Cleanup(guest.Close)
	ctx, cancel := context.WithCancel(context.Background())
	proxy, err := start(ctx, func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", guest.Listener.Addr().String())
	}, ttl)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if err := proxy.Wait(); err != nil {
			t.Error(err)
		}
	})
	return proxy, cancel
}

func noRedirectClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar: jar, Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func send(t *testing.T, client *http.Client, req *http.Request, want int) *http.Response {
	t.Helper()
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("status = %d, want %d", response.StatusCode, want)
	}
	return response
}

func request(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestBrowserAuthorization(t *testing.T) {
	received := make(chan http.Header, 1)
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			received <- r.Header.Clone()
		}
		w.Header().Set("Set-Cookie", "guest=value; Path=/")
		fmt.Fprint(w, "guest display")
	}), time.Minute)
	client := noRedirectClient(t)
	link := request(t, proxy.BootstrapURL)
	origin := link.URL.Scheme + "://" + link.URL.Host
	scope := strings.TrimSuffix(link.URL.Path, "bootstrap")
	asset := origin + scope + "asset"

	req := request(t, proxy.BootstrapURL)
	req.Host = "unrelated.example"
	send(t, client, req, http.StatusMisdirectedRequest)
	send(t, client, request(t, asset), http.StatusUnauthorized)
	bootstrap := send(t, client, link, http.StatusSeeOther)
	cookies := bootstrap.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode ||
		cookies[0].Path != scope || cookies[0].Domain != "" {
		t.Fatalf("unexpected desktop cookie attributes")
	}
	if location := bootstrap.Header.Get("Location"); !strings.HasPrefix(location, scope+"?") ||
		strings.Contains(location, link.URL.Query().Get("token")) {
		t.Fatal("redirect must stay within the session scope and omit the bootstrap token")
	}
	send(t, client, request(t, proxy.BootstrapURL), http.StatusUnauthorized)
	send(t, client, request(t, origin+"/asset"), http.StatusUnauthorized)

	req = request(t, asset)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	send(t, client, req, http.StatusForbidden)
	req = request(t, asset)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Origin", "https://unrelated.example")
	send(t, client, req, http.StatusForbidden)

	req = request(t, asset)
	req.SetBasicAuth("local", "secret")
	req.Header.Set("Proxy-Authorization", "local proxy secret")
	response := send(t, client, req, http.StatusOK)
	headers := <-received
	for _, key := range []string{"Cookie", "Authorization", "Proxy-Authorization"} {
		if headers.Get(key) != "" {
			t.Errorf("%s reached the guest", key)
		}
	}
	if response.Header.Get("Set-Cookie") != "" {
		t.Error("guest set a browser cookie")
	}
}

func TestBootstrapExpires(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 0)
	send(t, noRedirectClient(t), request(t, proxy.BootstrapURL), http.StatusUnauthorized)
}

func TestBootstrapSingleUseConcurrent(t *testing.T) {
	token := bootstrapToken{value: "value"}
	var wg sync.WaitGroup
	results := make(chan bool, 10)
	for range cap(results) {
		wg.Go(func() { results <- token.exchange("value", time.Now().Add(time.Minute)) })
	}
	wg.Wait()
	close(results)
	successes := 0
	for used := range results {
		if used {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful exchanges = %d, want 1", successes)
	}
}

func TestCancellationClosesWebSocketAndListener(t *testing.T) {
	guestClosed := make(chan struct{})
	guest := http.NewServeMux()
	guest.HandleFunc("/", func(http.ResponseWriter, *http.Request) {})
	guest.Handle("/websockify", websocket.Handler(func(conn *websocket.Conn) {
		defer close(guestClosed)
		var message string
		if websocket.Message.Receive(conn, &message) == nil {
			_ = websocket.Message.Send(conn, message)
			_ = websocket.Message.Receive(conn, &message)
		}
	}))
	proxy, cancel := testProxy(t, guest, time.Minute)
	client := noRedirectClient(t)
	entry := send(t, client, request(t, proxy.BootstrapURL), http.StatusSeeOther)
	entryURL, err := entry.Location()
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + entryURL.Host
	config, err := websocket.NewConfig("ws://"+entryURL.Host+"/"+entryURL.Query().Get("path"), origin)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range client.Jar.Cookies(entryURL) {
		config.Header.Add("Cookie", cookie.String())
	}
	config.Dialer = &net.Dialer{Timeout: 3 * time.Second}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(conn, "ready"); err != nil {
		t.Fatal(err)
	}
	var message string
	if err := websocket.Message.Receive(conn, &message); err != nil || message != "ready" {
		t.Fatalf("proxied message = %q, %v", message, err)
	}
	cancel()
	if err := proxy.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Receive(conn, &message); err == nil {
		t.Error("viewer remained connected")
	}
	select {
	case <-guestClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("guest WebSocket remained connected")
	}
	addr, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	if socket, err := net.DialTimeout("tcp", addr.Host, time.Second); err == nil {
		socket.Close()
		t.Fatal("host listener remained open")
	}
}
