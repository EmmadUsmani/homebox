package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

func TestResize(t *testing.T) {
	before := vm.Size{CPUs: 2, MemoryGiB: 4, DiskGiB: 20}
	after := vm.Size{CPUs: 3, MemoryGiB: 5, DiskGiB: 24}
	for _, tc := range []struct {
		name      string
		changes   vm.Size
		status    vm.Status
		driverErr error
		want      vm.Size
		wantErr   string
	}{
		{name: "stopped", changes: after, want: after},
		{name: "running", changes: after, status: vm.StatusRunning, want: before, wantErr: "box stop main"},
		{name: "query running", status: vm.StatusRunning, want: before},
		{name: "shrink", changes: vm.Size{DiskGiB: 19}, want: before, wantErr: "cannot shrink"},
		{name: "extra CPUs", changes: vm.Size{CPUs: 9}, want: before, wantErr: "exceeds the host's 8 CPUs"},
		{name: "extra memory", changes: vm.Size{MemoryGiB: 15}, want: before, wantErr: "host limit of 14 GiB"},
		{name: "driver failure", changes: after, driverErr: errors.New("edit failed"), want: before, wantErr: "edit failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, driver, _, _ := fixture(t)
			record := state.Box{Name: "main", Driver: driver.Name(), CPUs: before.CPUs, MemoryGiB: before.MemoryGiB, DiskGiB: before.DiskGiB}
			if err := service.Store.Save(record); err != nil {
				t.Fatal(err)
			}
			if err := driver.Create(t.Context(), vm.Spec{Name: record.Name, CPUs: before.CPUs, MemoryGiB: before.MemoryGiB, DiskGiB: before.DiskGiB}); err != nil {
				t.Fatal(err)
			}
			if tc.status == vm.StatusRunning {
				if err := driver.Start(t.Context(), record.Name, vm.StartOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			driver.Errors["Resize"] = tc.driverErr
			size, err := service.Resize(t.Context(), record.Name, tc.changes)
			if tc.wantErr == "" {
				if err != nil || size != tc.want {
					t.Fatalf("Resize = %+v, %v; want %+v", size, err, tc.want)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Resize error = %v; want %q", err, tc.wantErr)
			}
			saved, err := service.Store.Load(record.Name)
			if err != nil {
				t.Fatal(err)
			}
			if saved.PendingResize != nil || (vm.Size{CPUs: saved.CPUs, MemoryGiB: saved.MemoryGiB, DiskGiB: saved.DiskGiB}) != tc.want || driver.Instances[record.Name].Size != tc.want {
				t.Fatalf("record or driver size disagrees with %+v: record %+v, driver %+v", tc.want, saved, driver.Instances[record.Name])
			}
		})
	}
}

func TestRecoverResize(t *testing.T) {
	before := vm.Size{CPUs: 2, MemoryGiB: 4, DiskGiB: 20}
	after := vm.Size{CPUs: 3, MemoryGiB: 5, DiskGiB: 24}
	for _, configured := range []vm.Size{before, after} {
		service, driver, _, _ := fixture(t)
		record := state.Box{Name: "main", Driver: driver.Name(), CPUs: before.CPUs, MemoryGiB: before.MemoryGiB, DiskGiB: before.DiskGiB, PendingResize: &after}
		if err := service.Store.Save(record); err != nil {
			t.Fatal(err)
		}
		driver.Instances = map[string]vm.Instance{record.Name: {Name: record.Name, Status: vm.StatusStopped, Size: configured}}
		if err := service.Start(t.Context(), record.Name); err == nil || !strings.Contains(err.Error(), "unfinished resize") {
			t.Fatalf("Start error = %v", err)
		}
		size, err := service.Resize(t.Context(), record.Name, vm.Size{})
		if err != nil || size != configured {
			t.Fatalf("Resize = %+v, %v; want %+v", size, err, configured)
		}
		saved, err := service.Store.Load(record.Name)
		if err != nil || saved.PendingResize != nil || (vm.Size{CPUs: saved.CPUs, MemoryGiB: saved.MemoryGiB, DiskGiB: saved.DiskGiB}) != configured {
			t.Fatalf("recovery not persisted: %+v, %v", saved, err)
		}
	}
}
