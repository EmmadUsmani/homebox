package lima

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

const fixtureHome = "/fixture/lima"

func fixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/list.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLifecycleCommands(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("unsupported Lima host")
	}
	runner := &execx.Fake{}
	runner.On("/bin/limactl list --json", fixture(t), nil)
	driver := New(Options{Home: fixtureHome, Limactl: "/bin/limactl", Runner: runner})
	ctx := t.Context()
	spec := sampleSpec()
	wrap := []string{"sandbox-exec", "-f", "profile.sb"}
	for _, step := range []func() error{
		func() error { return driver.Create(ctx, spec) },
		func() error { return driver.Start(ctx, spec.Name, vm.StartOptions{Wrap: wrap}) },
		func() error { return driver.Start(ctx, spec.Name, vm.StartOptions{}) },
		func() error { return driver.Stop(ctx, spec.Name) },
		func() error { return driver.Delete(ctx, spec.Name) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	tempFile := runner.Calls[0].Args[4]
	if filepath.Ext(tempFile) != ".yaml" {
		t.Fatalf("expected temporary YAML, got %s", tempFile)
	}
	if _, err := os.Stat(tempFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary YAML cleanup: %v", err)
	}
	assertCommands(t, runner, []string{
		"/bin/limactl create --tty=false --name test-lima " + tempFile,
		"/bin/limactl list --json",
		"sandbox-exec -f profile.sb /bin/limactl start --tty=false test-lima",
		"/bin/limactl list --json",
		"/bin/limactl start --tty=false test-lima",
		"/bin/limactl list --json",
		"/bin/limactl stop test-lima",
		"/bin/limactl delete --force test-lima",
	})
}

func TestListAndSSH(t *testing.T) {
	runner := &execx.Fake{}
	runner.On("limactl list --json", fixture(t)+
		`{"name":"test-uninitialized","status":"Uninitialized","dir":"/fixture/lima/test-uninitialized"}`+"\n"+
		`{"name":"test-stopped","status":"Stopped","dir":"/fixture/lima/test-stopped"}`+"\n"+
		`{"name":"test-broken","status":"Broken","errors":["invalid config"],"dir":"/fixture/lima/test-broken"}`, nil)
	driver := New(Options{Home: fixtureHome, Runner: runner})
	ctx := t.Context()
	got, err := driver.List(ctx)
	want := []vm.Instance{
		{Name: "test-lima", Status: vm.StatusRunning, RuntimeDir: "/fixture/lima/test-lima", HostPIDFile: "/fixture/lima/test-lima/ha.pid", HostAddress: hostAddress},
		{Name: "test-uninitialized", Status: vm.StatusStopped, RuntimeDir: "/fixture/lima/test-uninitialized", HostPIDFile: "/fixture/lima/test-uninitialized/ha.pid", HostAddress: hostAddress},
		{Name: "test-stopped", Status: vm.StatusStopped, RuntimeDir: "/fixture/lima/test-stopped", HostPIDFile: "/fixture/lima/test-stopped/ha.pid", HostAddress: hostAddress},
		{Name: "test-broken", Status: vm.StatusBroken, RuntimeDir: "/fixture/lima/test-broken", HostPIDFile: "/fixture/lima/test-broken/ha.pid", HostAddress: hostAddress},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %#v, %v", got, err)
	}
	instance, err := driver.Get(ctx, "test-lima")
	if err != nil || instance != want[0] {
		t.Fatalf("Get = %#v, %v", instance, err)
	}
	endpoint, err := driver.SSH(ctx, "test-lima")
	wantEndpoint := vm.Endpoint{
		Host: "127.0.0.1", Port: 62659, User: "box",
		IdentityFile: "/fixture/lima/_config/user", KnownHosts: "/fixture/lima/test-lima/ssh.known_hosts",
	}
	if err != nil || endpoint != wantEndpoint {
		t.Fatalf("SSH = %#v, %v", endpoint, err)
	}
	if _, err := driver.SSH(ctx, "test-stopped"); err == nil {
		t.Fatal("expected SSH to reject a stopped instance")
	}
	assertCommands(t, runner, []string{"limactl list --json", "limactl list --json", "limactl list --json", "limactl list --json"})
}

func TestSetForwards(t *testing.T) {
	runner := &execx.Fake{}
	runner.On("limactl list --json", fixture(t)+
		`{"name":"test-stopped","status":"Stopped","dir":"/fixture/lima/test-stopped"}`, nil)
	driver := New(Options{Home: fixtureHome, Runner: runner})
	ctx := t.Context()
	if err := driver.SetForwards(ctx, "test-lima", nil); err == nil {
		t.Fatal("SetForwards accepted a running VM")
	}
	if err := driver.SetForwards(ctx, "test-stopped", []vm.PortForward{
		{GuestPort: 8000, HostPort: 10100, HostIP: "127.0.0.1"},
	}); err != nil {
		t.Fatal(err)
	}
	assertCommands(t, runner, []string{
		"limactl list --json",
		"limactl list --json",
		`limactl edit --tty=false --set .portForwards = [{"guestPort":8000,"hostPort":10100,"hostIP":"127.0.0.1","proto":"tcp","static":true},{"guestIP":"0.0.0.0","proto":"any","ignore":true},{"guestIP":"127.0.0.1","proto":"any","ignore":true},{"guestIP":"::","proto":"any","ignore":true}] test-stopped`,
	})
}

func TestMissingInstance(t *testing.T) {
	runner := &execx.Fake{}
	driver := New(Options{Home: fixtureHome, Runner: runner})
	ctx := t.Context()
	for _, step := range []func() error{
		func() error { _, err := driver.Get(ctx, "missing"); return err },
		func() error { _, err := driver.SSH(ctx, "missing"); return err },
		func() error { return driver.Start(ctx, "missing", vm.StartOptions{}) },
		func() error { return driver.Stop(ctx, "missing") },
		func() error { return driver.SetForwards(ctx, "missing", nil) },
	} {
		if err := step(); !errors.Is(err, vm.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	}
	if err := driver.Delete(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	assertCommands(t, runner, []string{
		"limactl list --json", "limactl list --json", "limactl list --json",
		"limactl list --json", "limactl list --json", "limactl delete --force missing",
	})
}

func TestListErrors(t *testing.T) {
	t.Run("invalid output", func(t *testing.T) {
		runner := &execx.Fake{}
		runner.On("limactl list --json", fixture(t)+"bad row", nil)
		driver := New(Options{Home: fixtureHome, Runner: runner})
		if _, err := driver.List(t.Context()); err == nil || !strings.Contains(err.Error(), "decode limactl list") {
			t.Fatalf("List error = %v", err)
		}
	})
	t.Run("command failure", func(t *testing.T) {
		runner := &execx.Fake{}
		failure := errors.New("permission denied")
		runner.On("limactl list --json", "", failure)
		driver := New(Options{Home: fixtureHome, Runner: runner})
		if _, err := driver.List(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("List error = %v", err)
		}
	})
}

func assertCommands(t *testing.T, runner *execx.Fake, want []string) {
	t.Helper()
	if got := runner.Commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	for _, call := range runner.Calls {
		if !reflect.DeepEqual(call.Env, []string{"LIMA_HOME=" + fixtureHome}) {
			t.Errorf("%s env = %q", call.Name, call.Env)
		}
	}
}

func TestStopMissingAfterLookup(t *testing.T) {
	const stderr = `level=fatal msg="open /fixture/lima/test-lima/lima.yaml: no such file or directory"`
	runner := &execx.Fake{Responses: []execx.FakeResponse{{
		Prefix: "limactl stop test-lima",
		Result: execx.Result{Stderr: []byte(stderr), ExitCode: 1},
		Err:    &execx.ExitError{ExitCode: 1, Stderr: stderr},
	}}}
	runner.On("limactl list --json", fixture(t), nil)
	driver := New(Options{Home: fixtureHome, Runner: runner})
	if err := driver.Stop(t.Context(), "test-lima"); !errors.Is(err, vm.ErrNotFound) {
		t.Fatalf("Stop error = %v, want ErrNotFound", err)
	}
}

func TestMissingHome(t *testing.T) {
	runner := &execx.Fake{}
	driver := New(Options{Runner: runner})
	if _, err := driver.List(t.Context()); err == nil {
		t.Fatal("List allowed an unset Lima home")
	}
	assertCommands(t, runner, []string{})
}
