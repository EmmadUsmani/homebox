// Package displayproxy serves the guest's KasmVNC web client over a caller-owned
// pinned SSH connection. Start takes a dialer for the fixed guest Endpoint and
// returns a single-use BootstrapURL. The caller cancels the context if SSH is
// lost, and Wait reports completion after the listener and viewers close.
package displayproxy

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Endpoint is the only guest address that a desktop tunnel should dial.
const Endpoint = "127.0.0.1:8444"

const bootstrapTTL = time.Minute

// Proxy is one browser session on an OS-assigned loopback port.
type Proxy struct {
	BootstrapURL string
	done         chan struct{}
	err          error
}

// Wait blocks until the proxy closes. Cancellation is a normal shutdown.
func (p *Proxy) Wait() error {
	<-p.done
	return p.err
}

// Start checks guest HTTP readiness, then serves until ctx is canceled.
// dial must honor cancellation and connect only to Endpoint over pinned SSH.
func Start(ctx context.Context, dial func(context.Context) (net.Conn, error)) (*Proxy, error) {
	return start(ctx, dial, bootstrapTTL)
}

func start(ctx context.Context, dial func(context.Context) (net.Conn, error), ttl time.Duration) (*Proxy, error) {
	return startWithPolicy(ctx, dial, ttl, false)
}

func startWithPolicy(ctx context.Context, dial func(context.Context) (net.Conn, error), ttl time.Duration, viewer bool) (*Proxy, error) {
	transport := &http.Transport{
		DialContext:           func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	target := &url.URL{Scheme: "http", Host: Endpoint}
	if err := ready(ctx, transport, target.String()); err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	host := listener.Addr().String()
	origin := "http://" + host
	scope := "/" + rand.Text() + "/"
	token := bootstrapToken{value: rand.Text()}
	session := rand.Text()
	bootstrap := scope + "bootstrap"
	query := url.Values{
		"autoconnect":   {"1"},
		"enable_webrtc": {"false"},
		"path":          {strings.TrimPrefix(scope, "/") + "websockify"},
	}
	entry := scope + "?" + query.Encode()
	expires := time.Now().Add(ttl)

	upstream := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Proxy-Authorization")
		},
		ModifyResponse: func(r *http.Response) error {
			r.Header.Del("Set-Cookie")
			r.Header.Del("Clear-Site-Data")
			r.Header.Set("Cache-Control", "no-store")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "guest desktop unavailable", http.StatusBadGateway)
		},
	}
	scoped := http.StripPrefix(strings.TrimSuffix(scope, "/"), upstream)
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Permissions-Policy", "clipboard-read=(), clipboard-write=()")
			if viewer {
				w.Header().Add("Content-Security-Policy", fmt.Sprintf(
					"default-src 'self'; connect-src 'self' ws://%s; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; worker-src 'self' blob:; frame-src 'none'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; sandbox allow-scripts allow-same-origin", host))
			}
			if r.Host != host {
				http.Error(w, "invalid desktop host", http.StatusMisdirectedRequest)
				return
			}
			upgrades := r.Header.Values("Upgrade")
			if len(upgrades) != 0 && (len(upgrades) != 1 || !strings.EqualFold(upgrades[0], "websocket")) {
				http.Error(w, "invalid desktop protocol upgrade", http.StatusBadRequest)
				return
			}
			origins := r.Header.Values("Origin")
			if (len(upgrades) != 0 || len(origins) != 0) && (len(origins) != 1 || origins[0] != origin) {
				http.Error(w, "invalid desktop origin", http.StatusForbidden)
				return
			}
			if r.URL.Path == bootstrap && r.Method == http.MethodGet {
				if !token.exchange(r.URL.Query().Get("token"), expires) {
					http.Error(w, "desktop link expired or already used", http.StatusUnauthorized)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name: "homebox-desktop", Value: session, Path: scope,
					HttpOnly: true, SameSite: http.SameSiteStrictMode,
				})
				http.Redirect(w, r, entry, http.StatusSeeOther)
				return
			}
			cookie, err := r.Cookie("homebox-desktop")
			if !strings.HasPrefix(r.URL.Path, scope) || err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(session)) != 1 {
				http.Error(w, "desktop session required", http.StatusUnauthorized)
				return
			}
			scoped.ServeHTTP(w, r)
		}),
	}
	p := &Proxy{
		BootstrapURL: origin + bootstrap + "?token=" + token.value,
		done:         make(chan struct{}),
	}
	connections := &trackedListener{Listener: listener, active: make(map[*trackedConn]struct{})}
	go func() {
		defer close(p.done)
		stopped := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(stopped)
			_ = server.Close()
		})
		if err := server.Serve(connections); !errors.Is(err, http.ErrServerClosed) {
			p.err = err
		}
		if !stop() {
			<-stopped
		}
		connections.closeActive()
		transport.CloseIdleConnections()
	}()
	return p, nil
}

func ready(ctx context.Context, transport *http.Transport, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		return fmt.Errorf("check guest desktop HTTP readiness: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("guest desktop HTTP readiness: %s", response.Status)
	}
	return nil
}

type bootstrapToken struct {
	value string
	mu    sync.Mutex
	used  bool
}

func (t *bootstrapToken) exchange(value string, expires time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.used || !time.Now().Before(expires) || subtle.ConstantTimeCompare([]byte(value), []byte(t.value)) != 1 {
		return false
	}
	t.used = true
	return true
}

// net/http leaves upgraded connections to the handler. Track them through
// Close so a viewer cannot keep the proxy alive after SSH disappears.
type trackedListener struct {
	net.Listener
	mu     sync.Mutex
	active map[*trackedConn]struct{}
}

func (l *trackedListener) Accept() (net.Conn, error) {
	raw, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	conn := &trackedConn{Conn: raw, listener: l}
	l.mu.Lock()
	l.active[conn] = struct{}{}
	l.mu.Unlock()
	return conn, nil
}

func (l *trackedListener) closeActive() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for conn := range l.active {
		_ = conn.Conn.Close()
	}
	clear(l.active)
}

type trackedConn struct {
	net.Conn
	listener *trackedListener
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.listener.mu.Lock()
	delete(c.listener.active, c)
	c.listener.mu.Unlock()
	return err
}
