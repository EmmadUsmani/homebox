package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func checkResize(t *testing.T, name, binary string, box func(time.Duration, ...string) string, run func(time.Duration, string, ...string) (string, error)) {
	t.Helper()
	const timeout = time.Minute
	measure := func() [3]int {
		t.Helper()
		output := box(timeout, "ssh", name, "--", `nproc; awk '/^MemTotal:/ {print $2}' /proc/meminfo; df -k --output=size / | tail -n 1`)
		fields := strings.Fields(output)
		if len(fields) != 3 {
			t.Fatalf("unexpected guest sizes: %q", output)
		}
		var sizes [3]int
		for i, field := range fields {
			value, err := strconv.Atoi(field)
			if err != nil {
				t.Fatal(err)
			}
			sizes[i] = value
		}
		return sizes
	}
	before := measure()
	box(2*time.Minute, "stop", name)
	box(timeout, "resize", name, "--cpus", "3", "--memory", "5", "--disk", "24")
	if output := box(timeout, "resize", name); !strings.Contains(output, "3 CPUs, 5 GiB memory, 24 GiB disk") {
		t.Fatalf("unexpected configured size: %s", output)
	}
	if output, err := run(timeout, binary, "resize", name, "--disk", "23"); err == nil || !strings.Contains(output, "cannot shrink") {
		t.Fatalf("expected disk shrink to be refused: %v\n%s", err, output)
	}
	box(3*time.Minute, "start", name)
	after := measure()
	t.Logf("Guest CPU / MemTotal KiB / root filesystem KiB: %v → %v", before, after)
	if before[0] != 2 || after[0] != 3 ||
		after[1]-before[1] < 950*1024 || after[1] > 5*1024*1024 ||
		after[2]-before[2] < 3*1024*1024 || after[2] > 24*1024*1024 {
		t.Fatal("guest CPU, memory or root filesystem did not grow to the requested size")
	}
}
