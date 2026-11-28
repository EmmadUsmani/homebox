package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

type imageDriver struct{ testDriver }

func (imageDriver) SaveDisk(_ context.Context, _, path string) ([]byte, error) {
	return nil, os.WriteFile(path, []byte("disk"), 0600)
}

func TestNewFromImage(t *testing.T) {
	service, driver, ssh, isolation := fixture(t)
	service.Driver = imageDriver{testDriver{driver}}
	ctx := t.Context()
	if err := service.New(ctx, NewOptions{Name: "source", DiskGiB: 64}); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveImage(ctx, "source", "saved", "test"); err == nil || !strings.Contains(err.Error(), "must be stopped") {
		t.Fatalf("save running source: %v", err)
	}
	if err := service.Stop(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveImage(ctx, "source", "saved", "test"); err != nil {
		t.Fatal(err)
	}
	if err := service.New(ctx, NewOptions{Name: "clone", Image: "saved", DiskGiB: 63}); err == nil || !strings.Contains(err.Error(), "64 GiB") {
		t.Fatalf("undersized clone: %v", err)
	}
	ssh.calls = nil
	if err := service.New(ctx, NewOptions{Name: "clone", Image: "saved"}); err != nil {
		t.Fatal(err)
	}
	spec := driver.Specs["clone"]
	source := driver.Specs["source"]
	if spec.DiskGiB != source.DiskGiB || spec.User != source.User || spec.ImageDisk == "" || spec.SSHPort == source.SSHPort {
		t.Fatalf("clone spec = %+v; source = %+v", spec, source)
	}
	if !isolation.Running("clone") || len(ssh.calls) != 0 {
		t.Fatalf("isolation ran = %t; guest provisioning calls = %v", isolation.Running("clone"), ssh.calls)
	}
	if err := service.RemoveImage("saved"); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(ctx, "clone"); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(ctx, "clone"); err != nil {
		t.Fatal(err)
	}
}

type baseTestProfiles struct {
	testProfiles
	revision string
}

func (p baseTestProfiles) Get(name string) (guest.Profile, error) {
	profile, err := p.testProfiles.Get(name)
	profile.ImageUpdate = []guest.Step{{Name: "refresh", Script: "refresh.sh"}}
	return profile, err
}

func (p baseTestProfiles) ReadScript(path string) ([]byte, error) {
	return []byte(path + p.revision), nil
}

type baseTestDriver struct{ imageDriver }

func (baseTestDriver) SupportsBaseImages() bool { return true }

func TestProfileBaseLifecycle(t *testing.T) {
	s, driver, ssh, _ := fixture(t)
	s.Driver = baseTestDriver{imageDriver{testDriver{driver}}}
	s.Profiles = baseTestProfiles{}
	s.DoctorHost = doctorHostFunc(func(_ context.Context, _ string, _ int, lan bool) ([]string, []doctor.Check, func()) {
		if lan {
			t.Error("base image build published on the LAN")
		}
		return nil, nil, func() {}
	})
	if err := s.Store.SaveHost(state.Host{LAN: true, AuthorizedKeys: []string{fixtureHostKey}}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := s.New(ctx, NewOptions{Name: "first", DiskGiB: 20}); err != nil {
		t.Fatal(err)
	}
	if driver.Specs["first"].ImageDisk == "" {
		t.Fatal("first VM did not use a rebuilt base")
	}
	count := 0
	for _, call := range ssh.calls {
		if call == "keys" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("authorized laptop keys %d times; want only the clone", count)
	}
	first, _, err := s.Store.LoadImage("base-ubuntu")
	if err != nil || first.BaseDigest == "" {
		t.Fatalf("missing base digest: %+v, %v", first, err)
	}
	boxes, err := s.Store.List()
	if err != nil || len(boxes) != 1 || boxes[0].Name != "first" {
		t.Fatalf("scratch VM was retained: %+v, %v", boxes, err)
	}
	ssh.calls = nil
	if err := s.New(ctx, NewOptions{Name: "second"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ssh.calls, []string{"keys", "refresh.sh"}) {
		t.Fatalf("current base ran guest steps %v", ssh.calls)
	}
	ssh.calls = nil
	if err := s.New(ctx, NewOptions{Name: "full", ForceProvision: true}); err != nil {
		t.Fatal(err)
	}
	if driver.Specs["full"].ImageDisk != "" || !slices.Equal(ssh.calls, []string{"keys", "sudo base.sh", "tools.sh"}) {
		t.Fatalf("force provision used image or unexpected steps: %v", ssh.calls)
	}

	s.Profiles = baseTestProfiles{revision: "2"}
	ssh.fail = errors.New("simulated provision failure")
	if err := s.RebuildImage(ctx, "", "failing"); err == nil {
		t.Fatal("rebuild succeeded despite failing provision step")
	}
	old, _, err := s.Store.LoadImage("base-ubuntu")
	if err != nil || old.BaseDigest != first.BaseDigest {
		t.Fatalf("failed build replaced the previous base: %+v, %v", old, err)
	}
	ssh.fail = nil
	if err := s.New(ctx, NewOptions{Name: "refreshed"}); err != nil {
		t.Fatal(err)
	}
	refreshed, _, err := s.Store.LoadImage("base-ubuntu")
	if err != nil || refreshed.BaseDigest == first.BaseDigest {
		t.Fatalf("stale image was not rebuilt: %+v, %v", refreshed, err)
	}
}

type socketLimitedBaseDriver struct {
	baseTestDriver
	root string
}

func (d socketLimitedBaseDriver) ValidateSocketPaths(name string) error {
	return vm.ValidateUnixSocketPath(filepath.Join(d.root, name, "ssh.sock.1234567890123456"))
}

func TestRebuildImageRejectsLongSocketPathBeforeCreatingState(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("host does not validate Unix socket paths")
	}
	service, driver, _, _ := fixture(t)
	service.Store.Root = filepath.Join(service.Store.Root, strings.Repeat("a", 108))
	service.Driver = socketLimitedBaseDriver{baseTestDriver{imageDriver{testDriver{driver}}}, filepath.Join(service.Store.Root, "lima")}
	service.Profiles = baseTestProfiles{}
	err := service.RebuildImage(t.Context(), "", "test")
	if err == nil || !strings.Contains(err.Error(), "shorter HOMEBOX_HOME") {
		t.Fatalf("expected a socket path error, got %v", err)
	}
	if _, err := os.Stat(service.Store.Root); !errors.Is(err, os.ErrNotExist) || len(driver.Specs) != 0 {
		t.Fatalf("rebuild created state or a VM: %v", err)
	}
}
