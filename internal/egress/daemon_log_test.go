package egress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonLogRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte("legacy log over limit"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, record := range []string{"first\n", "second\n", "third\n"} {
		log, err := openDaemonLog(path, 10)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte(record)); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte("oversized record")); err == nil {
			t.Fatal("oversized record accepted")
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{path: "third\n", path + ".1": "second\n"} {
		data, err := os.ReadFile(name)
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, %v; want %q", name, data, err, want)
		}
	}
}
