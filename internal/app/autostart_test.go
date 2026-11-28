package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

func TestAutostartRestoresMarkedBoxesDespiteFailures(t *testing.T) {
	service, driver, _, isolation := fixture(t)
	ctx := t.Context()
	for _, name := range []string{"first", "last", "unmarked"} {
		if err := service.New(ctx, NewOptions{Name: name}); err != nil {
			t.Fatal(err)
		}
		box, err := service.Store.Load(name)
		if err != nil || !box.Autostart {
			t.Fatalf("new %s mark = %t, err = %v", name, box.Autostart, err)
		}
		if err := service.Stop(ctx, name); err != nil {
			t.Fatal(err)
		}
		box, err = service.Store.Load(name)
		if err != nil || box.Autostart {
			t.Fatalf("stopped %s mark = %t, err = %v", name, box.Autostart, err)
		}
		if name == "unmarked" {
			continue
		}
		if err := service.Start(ctx, name); err != nil {
			t.Fatal(err)
		}
		box, err = service.Store.Load(name)
		if err != nil || !box.Autostart {
			t.Fatalf("started %s mark = %t, err = %v", name, box.Autostart, err)
		}
		// Simulate a shutdown that leaves the box record untouched.
		if err := driver.Stop(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	delete(driver.Instances, "first")
	badDir := service.Store.BoxDir("invalid")
	if err := os.Mkdir(badDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "box.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	isolation.calls = nil
	err := service.restoreMarked(ctx)
	if err == nil || !strings.Contains(err.Error(), "autostart first:") || !strings.Contains(err.Error(), "autostart invalid:") {
		t.Fatalf("restore error = %v", err)
	}
	if !slices.Equal(isolation.calls, []string{"up last"}) {
		t.Errorf("isolation calls = %v", isolation.calls)
	}
	if driver.Instances["last"].Status != vm.StatusRunning || driver.Instances["unmarked"].Status != vm.StatusStopped {
		t.Errorf("restore statuses = %v", driver.Instances)
	}
}
