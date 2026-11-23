package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
)

func TestParseLogLine(t *testing.T) {
	stamp := time.Date(2026, 11, 22, 1, 2, 3, 456000, time.UTC)
	arrival := stamp.Add(time.Hour)
	for _, tc := range []struct {
		source, raw, level, unit, text string
		time                           time.Time
	}{
		{"provision", stamp.Format(time.RFC3339Nano) + "\t\x1b[31mready\x1b[0m", "", "", "ready", stamp},
		{"host", `{"time":"2026-11-22T01:02:03.000456Z","level":"info","msg":"\u001b]0;fake\u0007booted","driver":"vz"}`, "info", "", "booted (driver=vz)", stamp},
		{"net", `{"time":"2026-11-22T01:02:03.000456Z","level":"WARN","msg":"denied","active":32,"reason":"guest\nvalue"}`, "WARN", "", "denied (reason=guest value, active=32)", stamp},
		{"guest", fmt.Sprintf(`{"__REALTIME_TIMESTAMP":"%d","MESSAGE":"hi\n\u001b[2Jthere","PRIORITY":"4","_SYSTEMD_UNIT":"worker\u001b[0m.service"}`, stamp.UnixMicro()), "warn", "worker.service", "hi there", stamp},
		{"guest", `{"MESSAGE":[0,27,255]}`, "", "", "[binary journal message]", arrival},
		{"host", "garbled\x1b[2J\x00 line", "", "", "garbled line", arrival},
		{"net", `{"time":"invalid","msg":"still useful"}`, "", "", "still useful", arrival},
	} {
		line := parseLogEntry(tc.source, tc.raw, false, arrival)
		if line.Source != tc.source || !line.Time.Equal(tc.time) || line.Level != tc.level || line.Unit != tc.unit || line.Text != tc.text {
			t.Errorf("%s log = %+v", tc.source, line)
		}
	}
	huge := parseLogEntry("guest", strings.Repeat("x", untrusted.MaxLine), true, arrival)
	if len(huge.Text) > untrusted.MaxLine || !strings.HasSuffix(huge.Text, " [line truncated]") {
		t.Fatal("large line was not visibly truncated")
	}
}

func TestLogsInterleaveAndStoppedGuest(t *testing.T) {
	service, driver, ssh, _ := fixture(t)
	const name = "main"
	if err := service.New(t.Context(), NewOptions{Name: name}); err != nil {
		t.Fatal(err)
	}
	instance := driver.Instances[name]
	instance.HostLog = filepath.Join(t.TempDir(), "host.log")
	driver.Instances[name] = instance
	writeLog(t, instance.HostLog, `{"time":"2026-11-22T01:02:02Z","msg":"boot"}`+"\n")
	runDir := service.Store.RunDir(name)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeLog(t, state.NetworkLogPath(runDir), `{"time":"2026-11-22T01:02:01Z","msg":"network"}`+"\n")
	run, ok, err := service.Store.LoadProvisionRun(name)
	if err != nil || !ok {
		t.Fatalf("load run = %+v, %t, %v", run, ok, err)
	}
	writeLog(t, filepath.Join(service.Store.ProvisionDir(name), run.Log), "2026-11-22T01:02:03Z\tprovisioned\n")
	ssh.stream = func(_ context.Context, _ []string, stdout, _ io.Writer) error {
		stamp, _ := time.Parse(time.RFC3339, "2026-11-22T01:02:04Z")
		_, err := fmt.Fprintf(stdout, `{"__REALTIME_TIMESTAMP":"%d","MESSAGE":"guest ready"}`+"\n", stamp.UnixMicro())
		return err
	}
	var lines []LogLine
	emit := func(line LogLine) error {
		lines = append(lines, line)
		return nil
	}
	if err := service.Logs(t.Context(), name, LogOptions{Lines: 1}, emit); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, line := range lines {
		sources = append(sources, line.Source)
	}
	if !slices.Equal(sources, []string{"net", "host", "provision", "guest"}) {
		t.Fatalf("sorted sources = %v", sources)
	}
	if err := service.Stop(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	lines = nil
	if err := service.Logs(t.Context(), name, LogOptions{Lines: 1}, emit); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 || lines[3].Source != "guest" || !strings.HasPrefix(lines[3].Text, "Guest journal unavailable") {
		t.Fatalf("stopped VM logs = %+v", lines)
	}
}

func TestJournalReconnectsAndCancels(t *testing.T) {
	for _, tc := range []struct {
		name, firstCursor, lastCursor string
		wantResume                    string
	}{
		{"resumes after latest cursor", "s=a;i=1", "s=a;i=2", "--after-cursor=s=a;i=2"},
		{"drops hostile cursor", "--evil=$(command)", "s=a;i=2\x1b[0m", ""},
		{"drops oversized cursor", strings.Repeat("a", 513), "", ""},
		{"keeps last valid cursor", "s=a;i=1", "s=a;i=2\n", "--after-cursor=s=a;i=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _, ssh, _ := fixture(t)
			if err := service.New(t.Context(), NewOptions{Name: "main"}); err != nil {
				t.Fatal(err)
			}
			writeEntry := func(out io.Writer, text, cursor string) error {
				return json.NewEncoder(out).Encode(map[string]string{"MESSAGE": text, "__CURSOR": cursor})
			}
			var commands [][]string
			ssh.stream = func(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
				commands = append(commands, slices.Clone(argv))
				if len(commands) == 1 {
					if err := writeEntry(stdout, "first", tc.firstCursor); err != nil {
						return err
					}
					if err := writeEntry(stdout, "last", tc.lastCursor); err != nil {
						return err
					}
					_, _ = io.WriteString(stderr, "connection lost\x1b[2J\n")
					return errors.New("disconnected")
				}
				if err := writeEntry(stdout, "reconnected", "s=a;i=3"); err != nil {
					return err
				}
				<-ctx.Done()
				return ctx.Err()
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var lines []string
			err := service.Logs(ctx, "main", LogOptions{Source: "guest", Unit: "worker.service", Lines: 2, Follow: true},
				func(line LogLine) error {
					lines = append(lines, line.Text)
					if line.Text == "reconnected" {
						cancel()
					}
					return nil
				})
			if !errors.Is(err, context.Canceled) || len(commands) != 2 ||
				!slices.Equal(lines, []string{"first", "last", "Guest journal unavailable: connection lost", "reconnected"}) {
				t.Fatalf("reconnect = %d attempts, %q, %v", len(commands), lines, err)
			}
			for _, args := range commands {
				if !slices.Contains(args, "--unit=worker.service") || !slices.Contains(args, "-f") {
					t.Fatalf("missing follow or unit in %q", args)
				}
			}
			initial, reconnect := commands[0], commands[1]
			if !slices.Equal(initial[len(initial)-2:], []string{"-n", "2"}) {
				t.Fatalf("initial command = %q", initial)
			}
			want := initial[:len(initial)-2]
			if tc.wantResume == "" {
				want = initial
			} else {
				want = append(slices.Clone(want), tc.wantResume)
			}
			if !slices.Equal(reconnect, want) {
				t.Fatalf("reconnect command = %q, want %q", reconnect, want)
			}
		})
	}
}

func TestProvisionRunRecordingAndRetention(t *testing.T) {
	service, driver, ssh, _ := fixture(t)
	const name = "main"
	currentRun := func() state.ProvisionRun {
		t.Helper()
		run, ok, err := service.Store.LoadProvisionRun(name)
		if err != nil || !ok || run.PID != os.Getpid() || run.FinishedAt == nil || run.FinishedAt.Before(run.StartedAt) || run.Log == "" {
			t.Fatalf("provision run = %+v, %t, %v", run, ok, err)
		}
		return run
	}
	driver.Errors["Create"] = errors.New("create failed")
	if err := service.New(t.Context(), NewOptions{Name: name}); err == nil {
		t.Fatal("creation should have failed")
	}
	first := currentRun()
	if first.Step != "create" || first.Error != "create failed" {
		t.Fatalf("failed creation run = %+v", first)
	}
	delete(driver.Errors, "Create")
	ssh.fail = errors.New("script failed")
	if err := service.Provision(t.Context(), name); err == nil {
		t.Fatal("script should have failed")
	}
	if failure := currentRun(); failure.Step != "base" || !strings.Contains(failure.Error, "script failed") {
		t.Fatalf("failed step run = %+v", failure)
	}
	ssh.fail = nil
	ssh.scriptOutput = "guest\x1b[2J output"
	for range 5 {
		if err := service.Provision(t.Context(), name); err != nil {
			t.Fatal(err)
		}
	}
	last := currentRun()
	dir := service.Store.ProvisionDir(name)
	logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(logs) != 5 || slices.Contains(logs, filepath.Join(dir, first.Log)) {
		t.Fatalf("retained logs = %v, %v", logs, err)
	}
	path := filepath.Join(dir, last.Log)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if last.Error != "" || last.Step != "tools" || info.Mode().Perm() != 0600 {
		t.Fatalf("successful run = %+v, mode %v", last, info.Mode())
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "Provisioned main") ||
		!strings.Contains(string(raw), "guest output") || strings.ContainsRune(string(raw), '\x1b') {
		t.Fatalf("provision log = %q, %v", raw, err)
	}
	for _, entry := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		stamp, _, ok := strings.Cut(entry, "\t")
		if _, err := time.Parse(time.RFC3339Nano, stamp); !ok || err != nil {
			t.Errorf("entry without timestamp: %q", entry)
		}
	}
}
