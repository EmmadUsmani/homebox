package lima

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrepareImageSSHKey(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("saved VZ images require macOS")
	}
	home := t.TempDir()
	driver := New(Options{Home: home})
	if err := driver.prepareImageSSHKey(t.Context()); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(home, "_config", "user")
	before, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0600 {
		t.Fatalf("private key permissions = %o", before.Mode().Perm())
	}
	if _, err := os.Stat(key + ".pub"); err != nil {
		t.Fatal(err)
	}
	if err := driver.prepareImageSSHKey(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(key)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("existing private key replaced: %v", err)
	}
}
