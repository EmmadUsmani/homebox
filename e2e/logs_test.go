package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
)

func checkLogs(t *testing.T, binary, name string, box func(time.Duration, ...string) string) {
	t.Helper()
	for _, source := range []string{"provision", "host", "net"} {
		output := box(time.Minute, "logs", name, "--source", source, "-n", "20")
		if !strings.Contains(output, " "+source+" ") {
			t.Fatalf("missing %s logs: %q", source, output)
		}
		if source == "provision" {
			if !strings.Contains(output, "Provisioned "+name) {
				t.Fatalf("missing provisioning completion: %q", output)
			}
			lines := strings.Split(strings.TrimSpace(output), "\n")
			t.Logf("plain log example: %s", lines[len(lines)-1])
		}
	}

	const marker = "homebox-e2e-journal-follow"
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	follow := exec.CommandContext(ctx, binary, "logs", name, "-f", "--source", "guest", "-n", "1", "--json")
	stdout, err := follow.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := follow.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = follow.Wait()
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("follow produced no journal history: %v", scanner.Err())
	}
	checkGuestLog(t, scanner.Text())
	box(5*time.Second, "ssh", name, "--", "logger -- "+marker)
	markerTimer := time.AfterFunc(10*time.Second, cancel)
	defer markerTimer.Stop()
	for scanner.Scan() {
		line := checkGuestLog(t, scanner.Text())
		if line.Text == marker {
			t.Logf("JSON log example: %s", scanner.Text())
			if err := follow.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			// Drain stdout so the process can finish regardless of its final writes.
			for scanner.Scan() {
			}
			if err := follow.Wait(); err != nil {
				t.Fatalf("interrupting follow: %v", err)
			}
			output := box(time.Minute, "logs", name, "--source", "guest", "--json", "-n", "20")
			for _, raw := range strings.Split(strings.TrimSpace(output), "\n") {
				if checkGuestLog(t, raw).Text == marker {
					return
				}
			}
			t.Fatal("journal snapshot omitted the follow marker")
		}
	}
	t.Fatal(fmt.Errorf("guest journal follow did not show the marker: %w", ctx.Err()))
}

func checkGuestLog(t *testing.T, raw string) app.LogLine {
	t.Helper()
	var line app.LogLine
	if err := json.Unmarshal([]byte(raw), &line); err != nil {
		t.Fatalf("invalid log JSON %q: %v", raw, err)
	}
	if line.Source != "guest" || line.Time.IsZero() || strings.HasPrefix(line.Text, "Guest journal unavailable") {
		t.Fatalf("expected genuine guest journal entry, got %+v", line)
	}
	return line
}
