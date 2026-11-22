package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/EmmadUsmani/homebox/internal/vm/vmtest"
)

type testProfiles struct{}

func (testProfiles) Names() []string { return []string{guest.DefaultProfile} }
func (testProfiles) Get(name string) (guest.Profile, error) {
	if name != guest.DefaultProfile {
		return guest.Profile{}, errors.New("unknown profile")
	}
	return guest.Profile{
		Name: name, Images: []vm.Image{{Arch: "aarch64", URL: "https://example.com/disk"}},
		Steps: []guest.Step{{Name: "base", Script: "base.sh", Root: true}, {Name: "tools", Script: "tools.sh"}},
	}, nil
}
func (testProfiles) ReadScript(path string) ([]byte, error) { return []byte(path), nil }

type testSSH struct {
	calls []string
	keys  []string
	env   map[string]string
	argv  []string
	fail  error
}

func (s *testSSH) RunScript(_ context.Context, _ vm.Endpoint, script []byte, env map[string]string, sudo bool, _, _ io.Writer) error {
	call := string(script)
	if sudo {
		call = "sudo " + call
	}
	s.calls = append(s.calls, call)
	s.env = env
	return s.fail
}

func (s *testSSH) AuthorizeKeys(_ context.Context, _ vm.Endpoint, keys []string) error {
	s.calls = append(s.calls, "keys")
	s.keys = slices.Clone(keys)
	return nil
}

func (s *testSSH) Interactive(_ vm.Endpoint, argv []string) error {
	s.argv = slices.Clone(argv)
	return nil
}

type testIsolation struct {
	calls    []string
	failDown error
}

func (i *testIsolation) Up(_ context.Context, box state.Box, instance vm.Instance) (Session, error) {
	if box.Name != instance.Name || instance.Status != vm.StatusStopped {
		return Session{}, errors.New("wrong instance for confinement")
	}
	i.calls = append(i.calls, "up "+box.Name)
	return Session{Start: vm.StartOptions{Wrap: []string{"test-wrapper"}}, ProxyURL: "http://proxy.example", NoProxy: "localhost", DNS: "192.0.2.53:53"}, nil
}

func (i *testIsolation) Down(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	i.calls = append(i.calls, "down "+name)
	return i.failDown
}

func fixture(t *testing.T) (*Service, *vmtest.Driver, *testSSH, *testIsolation) {
	t.Helper()
	driver := &vmtest.Driver{Errors: map[string]error{}}
	ssh := &testSSH{}
	isolation := &testIsolation{}
	return &Service{
		Driver: driver, Profiles: testProfiles{}, Store: &state.Store{Root: t.TempDir()}, GuestSSH: ssh,
		Isolation: isolation, Progress: &bytes.Buffer{},
		LocalHostName: func(context.Context) (string, error) { return "example.local", nil },
	}, driver, ssh, isolation
}

func TestNewProvisionListAndSSH(t *testing.T) {
	service, driver, ssh, isolation := fixture(t)
	if err := service.Store.SaveHost(state.Host{LAN: true, AuthorizedKeys: []string{"test-key"}}); err != nil {
		t.Fatal(err)
	}
	if err := service.New(t.Context(), NewOptions{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	spec := driver.Specs["main"]
	if spec.CPUs != 4 || spec.MemoryGiB != 8 || spec.DiskGiB != 50 || spec.SSHPort != 32201 || !spec.HostGateway || !guestUser.MatchString(spec.User) ||
		len(spec.Images) != 1 || len(spec.Forwards) != 100 || spec.Forwards[0].HostIP != "0.0.0.0" {
		t.Fatalf("unexpected create spec: %+v", spec)
	}
	if !slices.Equal(ssh.calls, []string{"keys", "sudo base.sh", "tools.sh"}) || !slices.Equal(ssh.keys, []string{"test-key"}) {
		t.Fatalf("unexpected guest operations: %+v", ssh)
	}
	wantEnv := map[string]string{"HOMEBOX_PROXY_URL": "http://proxy.example", "HOMEBOX_NO_PROXY": "localhost", "HOMEBOX_DNS": "192.0.2.53:53", "HOMEBOX_USER": spec.User}
	if !reflect.DeepEqual(ssh.env, wantEnv) {
		t.Fatalf("provision env = %v", ssh.env)
	}
	boxes, err := service.List(t.Context())
	if err != nil || len(boxes) != 1 || !boxes[0].Provisioned || boxes[0].ProvisionedAt == nil || boxes[0].Status != vm.StatusRunning {
		t.Fatalf("listing = %+v, %v", boxes, err)
	}
	if err := service.Provision(t.Context(), "main"); err != nil || len(ssh.calls) != 6 || len(isolation.calls) != 1 || !reflect.DeepEqual(ssh.env, wantEnv) {
		t.Fatalf("reprovision: %v, ssh %v, isolation %v", err, ssh.calls, isolation.calls)
	}
	for host, want := range map[string]string{"": "example.local", "vm-host.example": "vm-host.example"} {
		config, err := service.SSHConfig(t.Context(), host)
		if err != nil || !strings.Contains(config, "HostName "+want+"\n") {
			t.Fatalf("ssh-config = %q, %v", config, err)
		}
	}
	if _, err := service.SSHConfig(t.Context(), "example.local\n  Bad"); err == nil {
		t.Fatal("accepted newline in SSH config host")
	}
	argv := []string{"echo", "two words"}
	if err := service.SSH(t.Context(), "main", argv); err != nil || !slices.Equal(ssh.argv, argv) {
		t.Fatalf("ssh argv = %v, %v", ssh.argv, err)
	}
	if err := service.New(t.Context(), NewOptions{Name: "main"}); err == nil {
		t.Fatal("duplicate creation succeeded")
	}
	if !strings.Contains(service.Progress.(*bytes.Buffer).String(), "Provisioning main: tools") {
		t.Fatal("missing provision progress")
	}
}

func TestRetryCreationAndProvision(t *testing.T) {
	service, driver, ssh, _ := fixture(t)
	createFailure := errors.New("image error")
	driver.Errors["Create"] = createFailure
	err := service.New(t.Context(), NewOptions{Name: "main", CPUs: 2, MemoryGiB: 3, DiskGiB: 20})
	if !errors.Is(err, createFailure) || !strings.Contains(err.Error(), "box provision main") || !strings.Contains(err.Error(), "box rm main") {
		t.Fatalf("create failure = %v", err)
	}
	boxes, err := service.List(t.Context())
	if err != nil || len(boxes) != 1 || boxes[0].Status != vm.StatusBroken || boxes[0].Provisioned {
		t.Fatalf("failed creation listing = %+v, %v", boxes, err)
	}
	delete(driver.Errors, "Create")
	scriptFailure := errors.New("script failed")
	ssh.fail = scriptFailure
	if err := service.Provision(t.Context(), "main"); !errors.Is(err, scriptFailure) {
		t.Fatalf("script failure = %v", err)
	}
	spec := driver.Specs["main"]
	if driver.Instances["main"].Status != vm.StatusRunning || spec.CPUs != 2 || spec.MemoryGiB != 3 || spec.DiskGiB != 20 {
		t.Fatalf("failed provisioning did not preserve the VM or its size")
	}
	ssh.fail = nil
	if err := service.Provision(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleFailureAndCleanup(t *testing.T) {
	service, driver, _, isolation := fixture(t)
	startFailure := errors.New("start failed")
	driver.Errors["Start"] = startFailure
	if err := service.New(t.Context(), NewOptions{Name: "main"}); !errors.Is(err, startFailure) {
		t.Fatalf("start failure = %v", err)
	}
	if !slices.Equal(isolation.calls, []string{"up main", "down main"}) {
		t.Fatalf("cleanup after failure = %v", isolation.calls)
	}
	delete(driver.Errors, "Start")
	if err := service.Start(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	starts := slices.DeleteFunc(slices.Clone(driver.Calls), func(call vmtest.Call) bool { return call.Method != "Start" })
	last := starts[len(starts)-1]
	if !slices.Equal(last.Start.Wrap, []string{"test-wrapper"}) {
		t.Fatalf("launch wrapper = %+v", last)
	}
	driver.Errors["Stop"] = errors.New("stop failed")
	if err := service.Stop(t.Context(), "main"); err == nil || len(isolation.calls) != 3 {
		t.Fatalf("stop failure = %v, isolation calls = %v", err, isolation.calls)
	}
	delete(driver.Errors, "Stop")
	isolation.failDown = errors.New("down failed")
	if err := service.Remove(t.Context(), "main"); !errors.Is(err, isolation.failDown) {
		t.Fatalf("remove failure = %v", err)
	}
	if _, err := service.Store.Load("main"); err != nil {
		t.Fatalf("record missing before cleanup: %v", err)
	}
	isolation.failDown = nil
	if err := service.Remove(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Store.Load("main"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record after remove: %v", err)
	}
	if driver.Calls[len(driver.Calls)-3].Method != "Delete" {
		t.Fatalf("expected stop before delete: %+v", driver.Calls)
	}
}

func TestPortsAndHostSettings(t *testing.T) {
	service, driver, _, _ := fixture(t)
	var keyBytes = append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	keyBytes = append(keyBytes, []byte{0, 0, 0, 32}...)
	keyBytes = append(keyBytes, make([]byte, 32)...)
	key := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(keyBytes)
	path := filepath.Join(t.TempDir(), "public-key")
	if err := os.WriteFile(path, []byte(key+" comment\n"+key+" another comment\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lan := true
	if err := service.HostInit(t.Context(), HostInitOptions{LAN: &lan, AuthorizedKeyFiles: []string{path}}); err != nil {
		t.Fatal(err)
	}
	host, err := service.Store.LoadHost()
	if err != nil || !host.LAN || !slices.Equal(host.AuthorizedKeys, []string{key}) {
		t.Fatalf("host = %+v, %v", host, err)
	}
	for _, name := range []string{"main", "other"} {
		if err := service.New(t.Context(), NewOptions{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Ports(t.Context(), "main", PortOptions{Add: []string{"9000:30000"}}); err == nil {
		t.Fatal("edited a running VM")
	}
	if err := service.Stop(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"22:30000", "9000:10200", "9000:0", "9000:2201", "9000:2202"} {
		if _, err := service.Ports(t.Context(), "main", PortOptions{Set: []string{bad}}); err == nil {
			t.Errorf("accepted invalid forward %q", bad)
		}
	}
	forwards, err := service.Ports(t.Context(), "main", PortOptions{Set: []string{"9000:30000"}, Add: []string{"9001:30001"}})
	if err != nil || len(forwards) != 2 || forwards[0].HostIP != "0.0.0.0" {
		t.Fatalf("updated forwards = %v, %v", forwards, err)
	}
	driver.Errors["SetForwards"] = errors.New("driver write failed")
	if _, err := service.Ports(t.Context(), "main", PortOptions{Remove: []string{"9000"}}); err == nil {
		t.Fatal("expected a driver error")
	}
	recorded, err := service.Ports(t.Context(), "main", PortOptions{})
	if err != nil || !slices.Equal(recorded, forwards) {
		t.Fatalf("failed edit changed saved forwards = %v, %v", recorded, err)
	}
	delete(driver.Errors, "SetForwards")
	recorded, err = service.Ports(t.Context(), "main", PortOptions{Remove: []string{"9000"}})
	if err != nil || len(recorded) != 1 || recorded[0].GuestPort != 9001 || !slices.Equal(recorded, driver.Specs["main"].Forwards) {
		t.Fatalf("removed forward = %v, %v", recorded, err)
	}
}

func TestStartAppliesAChangedLANSetting(t *testing.T) {
	service, driver, _, _ := fixture(t)
	if err := service.New(t.Context(), NewOptions{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	lan := true
	if err := service.HostInit(t.Context(), HostInitOptions{LAN: &lan}); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	box, err := service.Store.Load("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, forwards := range [][]vm.PortForward{box.Forwards, driver.Specs["main"].Forwards} {
		if len(forwards) == 0 || forwards[0].HostIP != "0.0.0.0" || forwards[len(forwards)-1].HostIP != "0.0.0.0" {
			t.Fatalf("forwards not republished on the LAN: %v", forwards[:1])
		}
	}
}
