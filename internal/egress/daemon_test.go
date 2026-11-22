package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	for _, stop := range []string{"cancel", "lifeline"} {
		t.Run(stop, func(t *testing.T) {
			port, err := FreePort()
			if err != nil {
				t.Fatal(err)
			}
			listen := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			dir := t.TempDir()
			pidFile, logFile := filepath.Join(dir, "proxy.pid"), filepath.Join(dir, "proxy.log")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var alive atomic.Bool
			alive.Store(true)
			done := make(chan error, 1)
			go func() {
				done <- Run(ctx, Config{Listen: listen, PIDFile: pidFile, LogFile: logFile, Lifeline: alive.Load})
			}()

			deadline := time.After(5 * time.Second)
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			waiting := true
			for waiting {
				select {
				case <-deadline:
					t.Fatal("daemon did not publish its PID")
				case err := <-done:
					t.Fatalf("daemon exited early: %v", err)
				case <-ticker.C:
					pid, err := os.ReadFile(pidFile)
					waiting = err != nil || string(pid) != fmt.Sprintln(os.Getpid())
				}
			}
			socket, _, status := connectRequest(t, listen, "127.0.0.1:22", "")
			socket.Close()
			if status != http.StatusForbidden {
				t.Fatalf("daemon CONNECT returned %d", status)
			}
			if stop == "cancel" {
				cancel()
			} else {
				alive.Store(false)
			}
			select {
			case <-deadline:
				t.Fatal("daemon did not stop")
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
				t.Errorf("PID file remains: %v", err)
			}
			log, err := os.ReadFile(logFile)
			if err != nil || !strings.Contains(string(log), `"decision":"deny"`) {
				t.Errorf("daemon log %q, %v", log, err)
			}
		})
	}
}
