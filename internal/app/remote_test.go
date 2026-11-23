package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

func TestRemoteSSHDiagnostics(t *testing.T) {
	for _, state := range []RemoteState{RemoteUnreachable, RemoteStopped, RemoteBroken, RemoteKeyChanged} {
		t.Run(string(state), func(t *testing.T) {
			stderr, err := os.ReadFile(filepath.Join("testdata", "remote-ssh", string(state)+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			output := strings.ReplaceAll(string(stderr), "\n", "\r\n")
			if got := classifyRemote(execx.Result{Stderr: []byte(output), ExitCode: 255}, &execx.ExitError{ExitCode: 255}, nil); got != state {
				t.Fatalf("got %s, want %s", got, state)
			}
		})
	}
	if got := classifyRemote(execx.Result{}, nil, nil); got != RemoteReachable {
		t.Fatalf("successful login: %s", got)
	}
	if got := classifyRemote(execx.Result{}, &execx.ExitError{ExitCode: -1}, context.DeadlineExceeded); got != RemoteUnreachable {
		t.Fatalf("timed-out login: %s", got)
	}
}

func TestRemoteSharesGuestProbe(t *testing.T) {

	runner := &execx.Fake{}
	runner.On("/usr/bin/ssh", "uptime 3601\nmemory 100 25\nload 1 2 3\nprocess 55 1.2 15 \x1b[31mguest\nlistener 0.0.0.0:8000 users:((\"node\",pid=55,fd=3))    \nagent 11 codex /tmp", nil)
	source := RemoteSource{Runner: runner, Profiles: testProfiles{}, KnownHostsPath: "/test/pins", ConfigPath: "/test/config"}
	check, err := source.Check(t.Context(), "fixture-vm")
	if err != nil || check.State != RemoteReachable || check.Status == nil {
		t.Fatalf("check = %+v, err = %v", check, err)
	}
	status := check.Status
	if status.Guest == nil || status.Guest.Memory.UsedBytes != 75*1024 ||
		status.Guest.Processes[0].Name != "guest" || len(status.Agents) != 0 ||
		len(status.Ports) != 1 || status.Ports[0].Processes[0].Name != "node" ||
		status.Host != nil || status.Doctor != nil || status.Provisioning != nil {
		t.Fatalf("unexpected guest status: %+v", status)
	}
	args := runner.Calls[0].Args
	for _, option := range []string{"-a", "-x", "-T", "StrictHostKeyChecking=yes", "ControlPath=none", "ClearAllForwardings=yes", "BatchMode=yes", "UserKnownHostsFile=\"/test/pins\"", "/test/config"} {
		if !slices.Contains(args, option) {
			t.Fatalf("missing required SSH argument %q", option)
		}
	}
	if _, err := source.Check(t.Context(), "-oProxyCommand=bad"); err == nil || len(runner.Calls) != 1 {
		t.Fatal("invalid SSH alias was accepted")
	}
}
