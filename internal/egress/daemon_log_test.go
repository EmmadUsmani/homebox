package egress

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonLogRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte("legacy log over limit"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, record := range []string{"first\n", "second\n", "third\n"} {
		log, err := openDaemonLog(path, 10)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte(record)); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte("oversized record")); err == nil {
			t.Fatal("oversized record accepted")
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{path: "third\n", path + ".1": "second\n"} {
		data, err := os.ReadFile(name)
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, %v; want %q", name, data, err, want)
		}
	}
}

func TestProxyLogsBoundedQuotedTargets(t *testing.T) {
	var log bytes.Buffer
	proxy := NewProxy(ProxyOptions{Logger: slog.New(slog.NewTextHandler(&log, nil))})
	request := httptest.NewRequest(http.MethodConnect, "example.test:443", nil)
	request.RequestURI = "bad\r\n\x1b" + strings.Repeat("x", 1000) + "trailing"
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	line := log.String()
	if response.Code != http.StatusBadRequest || strings.Count(line, "\n") != 1 ||
		strings.ContainsAny(line, "\r\x1b") || strings.Contains(line, "trailing") ||
		!strings.Contains(line, "...") || len(line) > 512 {
		t.Fatalf("status %d, unsafe log %q", response.Code, line)
	}
}
