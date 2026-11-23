package hostinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

func TestHostagentUsageMatchesInstance(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "vm with spaces", "ha.pid")
	if err := os.MkdirAll(filepath.Dir(pidFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFile, []byte("123\n"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		want bool
	}{
		{pidFile, true},
		{pidFile + "-other", false},
		{filepath.Join(t.TempDir(), "other", "ha.pid"), false},
	} {
		runner := &execx.Fake{}
		runner.On("/bin/ps -ww -p 123", "2.5 1024 1-02:03:04 limactl hostagent --pidfile "+test.path+" --socket ha.sock vm\n", nil)
		usage := (ProcessUsage{Runner: runner}).Sample(t.Context(), pidFile)
		if test.want {
			if usage == nil || usage.CPUPercent != 2.5 || usage.ResidentBytes != 1<<20 || usage.UptimeSeconds != 93784 || usage.Source != "hostagent" {
				t.Fatalf("unexpected sample: %+v", usage)
			}
		} else if usage != nil {
			t.Fatalf("misattributed sample: %+v", usage)
		}
	}
}
