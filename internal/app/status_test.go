package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/hostinfo"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

func TestGuestStatus(t *testing.T) {
	input := "garbage\nuptime 15.81\nload 0.10 0.20 0.30\nmemory 2048 1024\ndisk 4096 3072\n" +
		"listener 0.0.0.0:8000 users:((\x1b[31mserver\x1b[0m))\nlistener [::]:8443\nlistener *:65536\n" +
		"process bad NaN 4 invalid\nprocess 1 1 4 oversized" + strings.Repeat("x", untrusted.MaxLine) + "\n" +
		strings.Repeat("process 12 1.25 8 \x1b[31mnode\x1b[0m\n", 40) +
		strings.Repeat("container 1 test ubuntu Up 2 minutes\n", 101) +
		"agent 7 codex /tmp/\x1b]0;unsafe\x07workspace\n"
	var status VMStatus
	parseGuestStatus(input, &status, true)
	if status.Guest == nil || status.Guest.Memory == nil || *status.Guest.UptimeSeconds != 15 ||
		status.Guest.Memory.UsedBytes != 1024*1024 || len(status.Guest.Processes) != 10 || status.Guest.Processes[0].Name != "node" {
		t.Fatalf("unexpected guest: %+v", status.Guest)
	}
	if len(status.Ports) != 2 || status.Ports[0].Process != "users:((server))" ||
		len(status.Containers) != 100 || len(status.Agents) != 1 || status.Agents[0].CWD != "/tmp/workspace" {
		t.Fatalf("unexpected detail: %+v", status)
	}
	matchPorts(status.Ports, []vm.PortForward{{GuestPort: 8000, HostPort: 11100}})
	if !status.Ports[0].Published || status.Ports[0].HostPort != 11100 ||
		status.Ports[0].HostURL != "http://127.0.0.1:11100/" || status.Ports[1].Published {
		t.Fatalf("unexpected publishing: %+v", status.Ports)
	}
	var light VMStatus
	parseGuestStatus(input, &light, false)
	if light.Guest == nil || len(light.Guest.Processes) != 0 || len(light.Ports) != 0 || len(light.Containers) != 0 || len(light.Agents) != 0 {
		t.Fatalf("unexpected light probe: %+v", light)
	}
	var invalid VMStatus
	parseGuestStatus("uptime -1\nload NaN Inf 1\nmemory 8 9\ndisk 8 invalid\n", &invalid, true)
	if invalid.Guest != nil {
		t.Fatalf("invalid measurements accepted: %+v", invalid.Guest)
	}
}

func TestCappedGuestOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	out := &cappedGuestOutput{limit: 6, cancel: cancel}
	source := struct{ io.Reader }{strings.NewReader("12345678901234")}
	count, err := io.Copy(out, source)
	if count != 6 || !errors.Is(err, errGuestOutputLimit) || ctx.Err() == nil || out.buffer.String() != "123456" {
		t.Fatalf("cap: count=%d err=%v cancelled=%v output=%q", count, err, ctx.Err(), out.buffer.String())
	}
}

func TestProvisionStatus(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		run   state.ProvisionRun
		alive bool
		want  string
	}{
		{state.ProvisionRun{PID: 9}, true, "provisioning"},
		{state.ProvisionRun{PID: 9}, false, "provision-interrupted"},
		{state.ProvisionRun{PID: 9, Error: "broken"}, true, "provision-failed"},
		{state.ProvisionRun{FinishedAt: &now, Error: "broken"}, false, "provision-failed"},
		{state.ProvisionRun{FinishedAt: &now}, false, "stopped"},
	} {
		got := provisionStatus("stopped", test.run, func(pid int) bool { return test.alive && pid == 9 })
		if got != test.want {
			t.Errorf("provisionStatus(%+v) = %q, want %q", test.run, got, test.want)
		}
	}
}

func TestStatusOfStoppedBox(t *testing.T) {
	service, driver, ssh, _ := fixture(t)
	box := state.Box{Name: "stopped", Driver: driver.Name()}
	if err := service.Store.Save(box); err != nil {
		t.Fatal(err)
	}
	driver.Instances = map[string]vm.Instance{box.Name: {Name: box.Name, Status: vm.StatusStopped}}
	now := time.Now().UTC()
	if err := service.Store.SaveDoctorResult(box, state.DoctorResult{CheckedAt: now, Passed: 7}); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(t.Context(), box.Name)
	if err != nil || status.State != "stopped" || status.Doctor == nil || status.Doctor.Passed != 7 || len(ssh.calls) != 0 {
		t.Fatalf("stopped status: %+v, err=%v, guest calls=%v", status, err, ssh.calls)
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "state", "doctor"} {
		if keys[field] == nil {
			t.Errorf("missing JSON field %q in %s", field, data)
		}
	}
	for _, field := range []string{"guest", "host", "uptime_seconds", "provisioning", "ports", "errors"} {
		if keys[field] != nil {
			t.Errorf("unexpected JSON field %q in %s", field, data)
		}
	}
}

func TestDenialsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmnet.log")
	var data strings.Builder
	data.WriteString("{\"time\":\"2026-01-01T00:00:00Z\",\"msg\":\"egress denied\",\"reason\":\"outdated\"}\n")
	data.WriteString(strings.Repeat("x", 128<<10))
	data.WriteString("\n{}\nbad JSON\n")
	for index := range 25 {
		fmt.Fprintf(&data, "{\"time\":\"2026-01-01T00:00:00Z\",\"msg\":\"egress denied\",\"protocol\":\"tcp\",\"destination\":\"destination-%d\"}\n", index)
	}
	data.WriteString("{\"time\":\"2026-01-01T00:00:00Z\",\"msg\":\"egress flow refused\",\"reason\":\"\\u001b[31mlimit\\u001b[0m\",\"active\":0,\"limit\":1}\n")
	data.WriteString("{\"time\":\"2026-01-01T00:00:00Z\",\"msg\":\"egress denied\"}")
	if err := os.WriteFile(path, []byte(data.String()), 0600); err != nil {
		t.Fatal(err)
	}
	denials, err := readDenials(path)
	if err != nil || len(denials) != 20 || denials[0].Reason != "limit" || denials[0].Active == nil ||
		*denials[0].Active != 0 || denials[1].Destination != "destination-24" || denials[19].Destination != "destination-6" {
		t.Fatalf("denials=%+v err=%v", denials, err)
	}
}

func TestStatusHostUsageFallback(t *testing.T) {
	agent := &hostinfo.Usage{Source: "hostagent", PID: 10, UptimeSeconds: 4}
	machine := &hostinfo.Usage{Source: "virtualization", PID: 20, UptimeSeconds: 3}
	var fallback VMStatus
	fallback.setHostUsage(hostinfo.UsageSample{HostAgent: agent})
	if fallback.Host != agent || fallback.HostAgent != nil || *fallback.UptimeSeconds != 4 {
		t.Fatalf("unexpected hostagent fallback: %+v", fallback)
	}
	uptime := uint64(2)
	status := VMStatus{UptimeSeconds: &uptime}
	status.setHostUsage(hostinfo.UsageSample{VM: machine, HostAgent: agent})
	if status.Host != machine || status.HostAgent != agent || *status.UptimeSeconds != 2 {
		t.Fatalf("unexpected VM metrics: %+v", status)
	}
}
