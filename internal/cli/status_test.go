package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/hostinfo"
)

func (*stubApp) Status(_ context.Context, name string) (app.VMStatus, error) {
	return app.VMStatus{Name: name, State: "running", Ports: []app.PortStatus{
		{Address: "*", GuestPort: 8000, Published: true, HostPort: 10100, HostURL: "http://127.0.0.1:10100/", Processes: []app.ListenerProcess{{Name: "python3", PID: 123}}},
	}}, nil
}

func TestStatusRendering(t *testing.T) {
	service := &stubApp{}
	table, _, err := execute(t, service, false, "", "status", "main")
	if err != nil || !strings.Contains(table, "TCP listener") || !strings.Contains(table, "http://127.0.0.1:10100/ python3 (pid 123)") {
		t.Fatalf("status table = %q, %v", table, err)
	}
	output, _, err := execute(t, service, false, "", "status", "main", "--json")
	var status app.VMStatus
	if decodeErr := json.Unmarshal([]byte(output), &status); err != nil || decodeErr != nil ||
		status.Name != "main" || status.State != "running" || len(status.Ports) != 1 || !status.Ports[0].Published || !strings.Contains(output, `"processes":[{"name":"python3","pid":123}]`) {
		t.Fatalf("status JSON = %q, %v, %v", output, err, decodeErr)
	}
	list, _, err := execute(t, service, false, "", "ls", "--json")
	var boxes []app.VMStatus
	if decodeErr := json.Unmarshal([]byte(list), &boxes); err != nil || decodeErr != nil || len(boxes) != 2 {
		t.Fatalf("ls JSON = %q, %v, %v", list, err, decodeErr)
	}
	var top bytes.Buffer
	if err := printTop(&top, app.TopSnapshot{VMs: []app.VMStatus{status}}); err != nil ||
		!strings.HasPrefix(top.String(), "\x1b[H\x1b[2JNAME") || !strings.Contains(top.String(), "main") {
		t.Fatalf("top table = %q, %v", &top, err)
	}
}

func TestProvisioningStatusRendering(t *testing.T) {
	finished := time.Date(2026, time.November, 22, 12, 34, 56, 0, time.UTC)
	for _, test := range []struct {
		run  app.ProvisionState
		want []string
		skip string
	}{
		{app.ProvisionState{Step: "Self-test", FinishedAt: &finished},
			[]string{"Provisioned", finished.Local().Format("2006-01-02 15:04:05 MST")}, "Self-test"},
		{app.ProvisionState{Step: "Self-test"}, []string{"Provisioning step", "Self-test"}, "Provisioned"},
		{app.ProvisionState{Step: "Self-test", FinishedAt: &finished, Error: "failed check"},
			[]string{"Provisioning step", "Self-test", "Provisioning error", "failed check"}, "Provisioned"},
	} {
		var output bytes.Buffer
		if err := printStatus(&output, app.VMStatus{Provisioning: &test.run}); err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(output.String(), want) {
				t.Errorf("output %q missing %q", &output, want)
			}
		}
		if strings.Contains(output.String(), test.skip) {
			t.Errorf("output %q includes %q", &output, test.skip)
		}
	}
}

func TestStatusHostMemoryRendering(t *testing.T) {
	footprint := uint64(4 << 30)
	var output bytes.Buffer
	status := app.VMStatus{Host: &hostinfo.Usage{Source: "virtualization", CPUPercent: 3, MemoryBytes: &footprint}}
	if err := printStatus(&output, status); err != nil || !strings.Contains(output.String(), "4096.0 MiB footprint") {
		t.Fatalf("status table = %q, %v", &output, err)
	}
	if _, memory := hostUsage(&hostinfo.Usage{Source: "hostagent"}); memory != "-" {
		t.Fatalf("unknown footprint = %q", memory)
	}
}
