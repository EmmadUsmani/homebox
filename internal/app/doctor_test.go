package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

type doctorHostFunc func(context.Context, string, int, bool) ([]string, []doctor.Check, func())

func (f doctorHostFunc) Probe(ctx context.Context, address string, port int, lan bool) ([]string, []doctor.Check, func()) {
	return f(ctx, address, port, lan)
}

type doctorIsolation struct {
	Isolation
	stopped string
}

func (i doctorIsolation) Running(name string) bool { return name != i.stopped }

func TestDoctor(t *testing.T) {
	service, driver, ssh, isolation := fixture(t)
	service.Isolation = doctorIsolation{Isolation: isolation, stopped: "broken"}
	if err := service.Store.SaveHost(state.Host{LAN: true}); err != nil {
		t.Fatal(err)
	}
	driver.Instances = make(map[string]vm.Instance)
	for _, name := range []string{"working", "stopped", "broken"} {
		status := vm.StatusRunning
		if name == "stopped" {
			status = vm.StatusStopped
		}
		driver.Instances[name] = vm.Instance{Name: name, Status: status, HostAddress: "192.0.2.10"}
		if err := service.Store.Save(state.Box{
			Name: name, Driver: driver.Name(), SSHPort: 2220,
			ProxyURL: "http://192.0.2.10:23020", DNS: "192.0.2.10:23120",
		}); err != nil {
			t.Fatal(err)
		}
	}
	probes, cleanups := 0, 0
	service.DoctorHost = doctorHostFunc(func(_ context.Context, address string, port int, lan bool) ([]string, []doctor.Check, func()) {
		probes++
		if address != "192.0.2.10" || port != 2220 || !lan {
			t.Fatalf("probe inputs = %q, %d, %t", address, port, lan)
		}
		return []string{"192.0.2.10:12345", "192.0.2.11:80"}, []doctor.Check{{Name: "canary", OK: true}}, func() { cleanups++ }
	})
	if err := service.Doctor(t.Context(), "working"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ssh.calls, []string{"common/doctor.sh"}) || probes != 1 || cleanups != 1 {
		t.Fatalf("calls = %v, probes = %d, cleanups = %d", ssh.calls, probes, cleanups)
	}
	wantEnv := map[string]string{
		"HOMEBOX_PROXY_URL":    "http://192.0.2.10:23020",
		"HOMEBOX_DNS":          "192.0.2.10:23120",
		"HOMEBOX_DENY_TARGETS": "192.0.2.10:12345 192.0.2.11:80",
		"HOMEBOX_PUBLIC_URL":   "https://example.com",
	}
	if !reflect.DeepEqual(ssh.env, wantEnv) {
		t.Fatalf("doctor environment = %v", ssh.env)
	}
	if err := service.Doctor(t.Context(), "stopped"); err == nil {
		t.Fatal("named stopped VM passed doctor")
	}

	guestFail := errors.New("guest diagnostic failed")
	ssh.fail = guestFail
	service.Progress.(*bytes.Buffer).Reset()
	err := service.Doctor(t.Context(), "")
	if !errors.Is(err, guestFail) || !strings.Contains(err.Error(), "host checks failed") {
		t.Fatalf("combined error = %v", err)
	}
	output := service.Progress.(*bytes.Buffer).String()
	for _, want := range []string{"Checking broken", "FAIL network-daemon", "Checking working", "PASS network-daemon"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output %q missing %q", output, want)
		}
	}
	if len(ssh.calls) != 3 || probes != 3 || cleanups != 3 || strings.Contains(output, "Checking stopped") {
		t.Fatalf("unexpected execution: %q, SSH %v, probes %d, cleanups %d", output, ssh.calls, probes, cleanups)
	}
}
