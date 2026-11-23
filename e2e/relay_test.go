package e2e

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// checkRelayDropsDeadClients kills SSH clients connected through box's relay
// and checks the guest's sshd sessions end with them instead of lingering.
func checkRelayDropsDeadClients(t *testing.T, config, name string, sessions func() string) {
	t.Helper()
	count := func() int {
		n, err := strconv.Atoi(strings.TrimSpace(sessions()))
		if err != nil {
			t.Fatalf("count guest SSH sessions: %v", err)
		}
		return n
	}
	before := count()
	for range 3 {
		client := exec.Command("ssh", "-F", config, "-tt", name, "bash -i")
		if _, err := client.StdinPipe(); err != nil {
			t.Fatal(err)
		}
		if err := client.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		_ = client.Process.Kill()
		_ = client.Wait()
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		after := count()
		if after <= before {
			t.Logf("guest SSH sessions: %d before, %d after 3 killed clients", before, after)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("guest SSH sessions grew from %d to %d after 3 killed clients", before, after)
		}
		time.Sleep(time.Second)
	}
}
