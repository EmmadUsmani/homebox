package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/app"
)

func (*stubApp) Status(_ context.Context, name string) (app.VMStatus, error) {
	return app.VMStatus{Name: name, State: "running", Ports: []app.PortStatus{
		{Address: "*", GuestPort: 8000, Published: true, HostPort: 10100, HostURL: "http://127.0.0.1:10100/"},
	}}, nil
}

func TestStatusRendering(t *testing.T) {
	service := &stubApp{}
	table, _, err := execute(t, service, false, "", "status", "main")
	if err != nil || !strings.Contains(table, "TCP listener") || !strings.Contains(table, "http://127.0.0.1:10100/") {
		t.Fatalf("status table = %q, %v", table, err)
	}
	output, _, err := execute(t, service, false, "", "status", "main", "--json")
	var status app.VMStatus
	if decodeErr := json.Unmarshal([]byte(output), &status); err != nil || decodeErr != nil ||
		status.Name != "main" || status.State != "running" || len(status.Ports) != 1 || !status.Ports[0].Published {
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
