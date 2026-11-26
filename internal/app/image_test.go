package app

import (
	"context"
	"os"
	"strings"
	"testing"
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
