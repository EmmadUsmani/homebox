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
	"strconv"
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

const fixtureHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type testSSH struct {
	hostKeys     []string
	calls        []string
	keys         []string
	env          map[string]string
	argv         []string
	fail         error
	scriptOutput string
	stream       func(context.Context, []string, io.Writer, io.Writer) error
}

func (s *testSSH) HostKeys(_ context.Context, _ vm.Endpoint) ([]string, error) {
	return slices.Clone(s.hostKeys), nil
}

type testDriver struct{ *vmtest.Driver }

func (d testDriver) SSH(ctx context.Context, name string) (vm.Endpoint, error) {
	ep, err := d.Driver.SSH(ctx, name)
	ep.IdentityFile = "/test/driver-id"
	return ep, err
}

func (s *testSSH) RunScript(_ context.Context, _ vm.Endpoint, script []byte, env map[string]string, sudo bool, stdout, _ io.Writer) error {
	call := string(script)
	if sudo {
		call = "sudo " + call
	}
	s.calls = append(s.calls, call)
	s.env = env
	if s.scriptOutput != "" {
		_, _ = io.WriteString(stdout, s.scriptOutput)
	}
	return s.fail
}

func (s *testSSH) AuthorizeKeys(_ context.Context, _ vm.Endpoint, keys []string) error {
	s.calls = append(s.calls, "keys")
	s.keys = slices.Clone(keys)
	return nil
}

func (s *testSSH) Stream(ctx context.Context, _ vm.Endpoint, argv []string, stdout, stderr io.Writer) error {
	if s.stream != nil {
		return s.stream(ctx, argv, stdout, stderr)
	}
	return nil
}

func (s *testSSH) Interactive(_ vm.Endpoint, argv []string) error {
	s.argv = slices.Clone(argv)
	return nil
}

type testIsolation struct {
	calls    []string
	failDown error
	up       map[string]bool
}

func (i *testIsolation) Up(_ context.Context, box state.Box, instance vm.Instance) (Session, error) {
	if box.Name != instance.Name {
		return Session{}, errors.New("wrong instance for confinement")
	}
	i.calls = append(i.calls, "up "+box.Name)
	if i.up == nil {
		i.up = map[string]bool{}
	}
	i.up[box.Name] = true
	return Session{Start: vm.StartOptions{Wrap: []string{"test-wrapper"}}, ProxyURL: "http://proxy.example", NoProxy: "localhost", DNS: "192.0.2.53:53"}, nil
}

func (i *testIsolation) Down(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	i.calls = append(i.calls, "down "+name)
	delete(i.up, name)
	return i.failDown
}

func (i *testIsolation) Running(name string) bool { return i.up[name] }

type testHostCapacity struct {
	cpus      int
	memoryGiB uint64
}

func (h testHostCapacity) Capacity() (int, uint64, error) {
	return h.cpus, h.memoryGiB << 30, nil
}

func fixture(t *testing.T) (*Service, *vmtest.Driver, *testSSH, *testIsolation) {
	t.Helper()
	driver := &vmtest.Driver{Errors: map[string]error{}}
	ssh := &testSSH{hostKeys: []string{fixtureHostKey}}
	isolation := &testIsolation{}
	return &Service{
		Driver: testDriver{driver}, Profiles: testProfiles{}, Store: &state.Store{Root: t.TempDir()}, GuestSSH: ssh,
		Isolation: isolation, Progress: &bytes.Buffer{},
		HostCapacity:  testHostCapacity{cpus: 8, memoryGiB: 16},
		LocalHostName: func(context.Context) (string, error) { return "example.local", nil },
	}, driver, ssh, isolation
}

func TestNewSizesVMForHost(t *testing.T) {
	for _, tc := range []struct {
		host     testHostCapacity
		wantCPUs int
		wantRAM  int
	}{
		{testHostCapacity{cpus: 8, memoryGiB: 8}, 4, 4},
		{testHostCapacity{cpus: 8, memoryGiB: 16}, 4, 8},
		{testHostCapacity{cpus: 16, memoryGiB: 64}, 4, 8},
		{testHostCapacity{cpus: 2, memoryGiB: 4}, 2, 2},
		{testHostCapacity{cpus: 4, memoryGiB: 7}, 4, 3},
		{testHostCapacity{cpus: 2, memoryGiB: 2}, 2, 2},
	} {
		t.Run(strconv.FormatUint(tc.host.memoryGiB, 10)+"GiB", func(t *testing.T) {
			service, driver, _, _ := fixture(t)
			service.HostCapacity = tc.host
			if err := service.New(t.Context(), NewOptions{Name: "main"}); err != nil {
				t.Fatal(err)
			}
			spec := driver.Specs["main"]
			if spec.CPUs != tc.wantCPUs || spec.MemoryGiB != tc.wantRAM {
				t.Errorf("VM size = %d CPUs and %d GiB, want %d CPUs and %d GiB",
					spec.CPUs, spec.MemoryGiB, tc.wantCPUs, tc.wantRAM)
			}
		})
	}
}

func TestNewRejectsSizesAboveHostCapacityBeforeCreating(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options NewOptions
		wantErr string
	}{
		{"memory", NewOptions{Name: "main", MemoryGiB: 7}, "host limit of 6 GiB (8 GiB RAM"},
		{"CPU", NewOptions{Name: "main", CPUs: 9}, "exceeds the host's 8 CPUs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, driver, _, isolation := fixture(t)
			service.HostCapacity = testHostCapacity{cpus: 8, memoryGiB: 8}
			if err := service.New(t.Context(), tc.options); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("New error = %v, want %q", err, tc.wantErr)
			}
			if _, err := service.Store.Load("main"); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("expected no box record, Load error = %v", err)
			}
			if len(driver.Specs) != 0 || len(isolation.calls) != 0 {
				t.Error("oversized VM reached the driver or isolation setup")
			}
		})
	}
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
	if spec.CPUs != 4 || spec.MemoryGiB != 8 || spec.DiskGiB != 50 {
		t.Errorf("default VM size = %d CPUs, %d GiB memory, %d GiB disk", spec.CPUs, spec.MemoryGiB, spec.DiskGiB)
	}
	if spec.SSHPort != 32201 || spec.HostGateway {
		t.Errorf("driver SSH port = %d, host gateway enabled = %t", spec.SSHPort, spec.HostGateway)
	}
	if !guestUser.MatchString(spec.User) {
		t.Errorf("invalid guest user %q", spec.User)
	}
	if len(spec.Images) != 1 {
		t.Errorf("got %d images, want 1", len(spec.Images))
	}
	if len(spec.Forwards) != 100 {
		t.Fatalf("got %d forwards, want 100", len(spec.Forwards))
	}
	if spec.Forwards[0].HostIP != "0.0.0.0" {
		t.Errorf("HTTP listen IP = %q, want LAN binding", spec.Forwards[0].HostIP)
	}
	if !slices.Equal(ssh.calls, []string{"keys", "sudo base.sh", "tools.sh"}) {
		t.Errorf("unexpected guest operations: %v", ssh.calls)
	}
	if !slices.Equal(ssh.keys, []string{"test-key"}) {
		t.Errorf("unexpected authorized keys: %v", ssh.keys)
	}
	wantEnv := map[string]string{
		"HOMEBOX_USER":           spec.User,
		"HOMEBOX_SOCKET_MAC":     "52:55:0a:72:00:15",
		"HOMEBOX_GATEWAY_SUBNET": "10.114.0.0/24",
	}
	if !reflect.DeepEqual(ssh.env, wantEnv) {
		t.Errorf("provision env = %v, want %v", ssh.env, wantEnv)
	}
	boxes, err := service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 1 {
		t.Fatalf("listing has %d boxes, want 1", len(boxes))
	}
	if !boxes[0].Provisioned || boxes[0].ProvisionedAt == nil {
		t.Errorf("listing is missing provisioning state: %+v", boxes[0])
	}
	if boxes[0].Status != vm.StatusRunning {
		t.Errorf("listing status = %q, want running", boxes[0].Status)
	}
	if err := service.Provision(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if len(ssh.calls) != 6 {
		t.Errorf("guest operations after reprovisioning = %v, want 6", ssh.calls)
	}
	if len(isolation.calls) != 1 {
		t.Errorf("isolation operations after reprovisioning = %v, want 1", isolation.calls)
	}
	if !reflect.DeepEqual(ssh.env, wantEnv) {
		t.Errorf("reprovision env = %v, want %v", ssh.env, wantEnv)
	}
	for host, want := range map[string]string{"": "example.local", "vm-host.example": "vm-host.example"} {
		config, err := service.SSHConfig(t.Context(), SSHConfigOptions{Host: host})
		if err != nil || !strings.Contains(config, "HostName "+want+"\n") {
			t.Fatalf("ssh-config = %q, %v", config, err)
		}
	}
	if _, err := service.SSHConfig(t.Context(), SSHConfigOptions{Host: "example.local\n  Bad"}); err == nil {
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
	for _, bad := range []string{"22:30000", "9000:10200", "9000:0", "9000:2201", "9000:2202", "9000:23050", "9000:23150", "9000:32250"} {
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

func TestRemoveDeletesAVMThatWontStop(t *testing.T) {
	service, driver, _, _ := fixture(t)
	if err := service.New(t.Context(), NewOptions{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	driver.Errors["Stop"] = errors.New("stop failed")
	if err := service.Remove(t.Context(), "main"); err != nil {
		t.Fatalf("remove = %v", err)
	}
	if _, err := driver.Get(t.Context(), "main"); !errors.Is(err, vm.ErrNotFound) {
		t.Fatalf("VM still exists: %v", err)
	}
}

func TestDeadNetworkDaemonRequiresVMRestart(t *testing.T) {
	service, _, _, isolation := fixture(t)
	if err := service.New(t.Context(), NewOptions{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(isolation.calls, []string{"up main"}) {
		t.Fatalf("healthy running VM touched isolation: %v", isolation.calls)
	}
	delete(isolation.up, "main") // the daemon died
	const recovery = "recover with `box stop main` then `box start main`"
	if err := service.Start(t.Context(), "main"); err == nil || !strings.Contains(err.Error(), recovery) {
		t.Fatalf("start error = %v", err)
	}
	status, err := service.Status(t.Context(), "main")
	if err != nil || !strings.Contains(strings.Join(status.Errors, "\n"), recovery) {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
	if !slices.Equal(isolation.calls, []string{"up main"}) {
		t.Fatalf("running VM touched isolation: %v", isolation.calls)
	}
	if err := service.Stop(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(isolation.calls, []string{"up main", "down main", "up main"}) || !isolation.Running("main") {
		t.Fatalf("network daemon not replaced on VM restart: %v", isolation.calls)
	}
}
