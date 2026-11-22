package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

func TestStoreRoundTrip(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	host, err := store.LoadHost()
	if err != nil || host.LAN || len(host.AuthorizedKeys) != 0 {
		t.Fatalf("default host = %+v, %v", host, err)
	}
	host = Host{LAN: true, AuthorizedKeys: []string{"test-public-key"}}
	if err := store.SaveHost(host); err != nil {
		t.Fatal(err)
	}
	if actual, err := store.LoadHost(); err != nil || !reflect.DeepEqual(actual, host) {
		t.Fatalf("host = %+v, %v", actual, err)
	}
	now := time.Now().UTC()
	box := Box{Name: "main", Profile: "ubuntu", Driver: "fake", CPUs: 4, MemoryGiB: 8, DiskGiB: 50,
		User: "developer", Index: 1, SSHPort: SSHPort(1), Forwards: DefaultForwards(1, host.LAN),
		CreatedAt: now, Provisioned: true, ProvisionedAt: &now}
	if err := store.Save(box); err != nil {
		t.Fatal(err)
	}
	if actual, err := store.Load(box.Name); err != nil || !reflect.DeepEqual(actual, box) {
		t.Fatalf("box round trip failed: %v", err)
	}
	if info, err := os.Stat(store.boxPath(box.Name)); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("box.json permissions = %v, %v", info, err)
	}
	runtime := filepath.Join(filepath.Dir(store.boxPath(box.Name)), "runtime")
	if err := os.WriteFile(runtime, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(box.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runtime); err != nil {
		t.Fatal(err)
	}
	if boxes, err := store.List(); err != nil || len(boxes) != 0 {
		t.Fatalf("list = %+v, %v", boxes, err)
	}
	if _, err := store.Load(box.Name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing box error = %v", err)
	}
}

func TestConcurrentAllocation(t *testing.T) {
	root := t.TempDir()
	var group sync.WaitGroup
	for _, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		group.Go(func() {
			store := &Store{Root: root}
			err := store.WithLock(func() error {
				index, err := store.NextIndex()
				if err != nil {
					return err
				}
				return store.Save(Box{Name: name, Index: index})
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	store := &Store{Root: root}
	boxes, err := store.List()
	if err != nil || len(boxes) != 4 {
		t.Fatalf("list = %+v, %v", boxes, err)
	}
	if next, err := store.NextIndex(); err != nil || next != 5 {
		t.Fatalf("next index = %d, %v", next, err)
	}
	if err := store.Delete(boxes[0].Name); err != nil {
		t.Fatal(err)
	}
	if next, err := store.NextIndex(); err != nil || next != boxes[0].Index {
		t.Fatalf("reused index = %d, %v", next, err)
	}
}

func TestNamesRootAndPorts(t *testing.T) {
	for _, name := range []string{"", "Main", "1main", "../main", strings.Repeat("a", 32), "my_vm"} {
		if ValidateName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if err := ValidateName("box-09"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("HOMEBOX_HOME", root)
	if got, err := DefaultRoot(); err != nil || got != root {
		t.Fatalf("root = %q, %v", got, err)
	}
	t.Setenv("HOMEBOX_HOME", "")
	t.Setenv("HOME", root)
	if got, err := DefaultRoot(); err != nil || got != filepath.Join(root, ".homebox") {
		t.Fatalf("default root = %q, %v", got, err)
	}
	if SSHPort(1) != 2201 || SSHPort(99) != 2299 {
		t.Fatal("SSH range differs from the design")
	}
	forwards := DefaultForwards(99, false)
	if len(forwards) != 100 || forwards[0] != (vm.PortForward{GuestPort: 8000, HostPort: 19900, HostIP: "127.0.0.1"}) ||
		forwards[99] != (vm.PortForward{GuestPort: 8099, HostPort: 19999, HostIP: "127.0.0.1"}) {
		t.Fatalf("unexpected forwards: %+v", forwards)
	}
	if DefaultForwards(1, true)[0].HostIP != "0.0.0.0" {
		t.Fatal("LAN forward does not bind all interfaces")
	}
}

func TestIndexExhaustion(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	for index := 1; index <= 99; index++ {
		name := "box-" + strconv.Itoa(index)
		if err := store.Save(Box{Name: name, Index: index}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.NextIndex(); err == nil {
		t.Fatal("expected exhausted indices")
	}
}

func TestNextIndexAtTestFloor(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	t.Setenv("HOMEBOX_FIRST_INDEX", "20")
	if err := store.Save(Box{Name: "one", Index: 20}); err != nil {
		t.Fatal(err)
	}
	if index, err := store.NextIndex(); err != nil || index != 21 {
		t.Fatalf("index = %d, %v, want 21", index, err)
	}
	t.Setenv("HOMEBOX_FIRST_INDEX", "100")
	if _, err := store.NextIndex(); err == nil {
		t.Fatal("accepted an out-of-range index floor")
	}
}
