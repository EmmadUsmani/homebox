package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
)

func checkStatus(t *testing.T, box func(time.Duration, ...string) string, binary, name, url string) {
	t.Helper()
	var status app.VMStatus
	output := box(12*time.Second, "status", name, "--json")
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, output)
	}
	if status.State != "running" || status.Guest == nil || status.Guest.Memory == nil || status.Doctor == nil ||
		status.Host == nil || status.Host.Source != "virtualization" || status.Host.PID <= 1 ||
		status.HostAgent == nil || status.HostAgent.Source != "hostagent" || status.HostAgent.PID == status.Host.PID {
		t.Fatalf("status is missing running VM data: %s", output)
	}
	published := false
	for _, port := range status.Ports {
		if port.GuestPort == 8000 && port.Published && port.HostURL == url {
			published = true
		}
	}
	if !published {
		t.Fatalf("status did not report the published listener: %s", output)
	}
	t.Logf("box status table:\n%s", box(12*time.Second, "status", name))
	t.Logf("box status JSON:\n%s", output)

	var list []app.VMStatus
	output = box(10*time.Second, "ls", "--json")
	if err := json.Unmarshal([]byte(output), &list); err != nil || len(list) != 1 || list[0].Name != name ||
		list[0].State != "running" || list[0].Guest != nil || list[0].Host == nil || list[0].Host.PID != status.Host.PID || list[0].UptimeSeconds == nil {
		t.Fatalf("unexpected host-only listing (%v): %s", err, output)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "top", name, "--json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var snapshot app.TopSnapshot
	decodeErr := json.NewDecoder(stdout).Decode(&snapshot)
	signalErr := cmd.Process.Signal(os.Interrupt)
	waitErr := cmd.Wait()
	if decodeErr != nil || signalErr != nil || waitErr != nil || len(snapshot.VMs) != 1 ||
		snapshot.VMs[0].Name != name || snapshot.VMs[0].Guest == nil ||
		snapshot.VMs[0].Host == nil || snapshot.VMs[0].Host.PID != status.Host.PID {
		t.Fatalf("top JSON: decode %v, interrupt %v, wait %v, snapshot %+v", decodeErr, signalErr, waitErr, snapshot)
	}
}
