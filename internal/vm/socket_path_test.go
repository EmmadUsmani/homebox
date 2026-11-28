package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateUnixSocketPath(t *testing.T) {
	maxBytes := 107
	switch runtime.GOOS {
	case "darwin":
		maxBytes = 103
	case "linux":
	default:
		t.Skip("host does not validate Unix socket paths")
	}
	allowed := "/" + strings.Repeat("a", maxBytes-1)
	if err := ValidateUnixSocketPath(allowed); err != nil {
		t.Fatalf("reject socket at the limit: %v", err)
	}
	if err := ValidateUnixSocketPath(allowed + "a"); err == nil || !strings.Contains(err.Error(), "shorter HOMEBOX_HOME") {
		t.Fatalf("overlong socket: %v", err)
	}

	temp := t.TempDir()
	target := filepath.Join(temp, strings.Repeat("a", maxBytes))
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(temp, "state")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "future-instance", "net.sock")
	if err := ValidateUnixSocketPath(path); err == nil || !strings.Contains(err.Error(), target) {
		t.Fatalf("overlong resolved socket: %v", err)
	}
}
