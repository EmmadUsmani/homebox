package cli

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

type stubApp struct {
	Application
	newOptions  app.NewOptions
	hostOptions app.HostInitOptions
	portOptions app.PortOptions
	logOptions  app.LogOptions
	argv        []string
	name        string
	sshOptions  app.SSHConfigOptions
}

func (s *stubApp) New(_ context.Context, options app.NewOptions) error {
	s.newOptions = options
	return nil
}

func (s *stubApp) HostInit(_ context.Context, options app.HostInitOptions) error {
	s.hostOptions = options
	return nil
}

func (s *stubApp) Remove(_ context.Context, name string) error {
	s.name = name
	return nil
}

func (s *stubApp) SSH(_ context.Context, name string, argv []string) error {
	s.name, s.argv = name, argv
	return nil
}

func (s *stubApp) Ports(_ context.Context, name string, options app.PortOptions) ([]vm.PortForward, error) {
	s.name, s.portOptions = name, options
	return []vm.PortForward{{GuestPort: 9000, HostPort: 30000, HostIP: "127.0.0.1"}}, nil
}

func (s *stubApp) Repin(_ context.Context, name string, confirm func(old, new []string) (bool, error)) error {
	approved, err := confirm([]string{"ssh-ed25519 SHA256:old"}, []string{"ssh-ed25519 SHA256:new"})
	if approved {
		s.name = name
	}
	return err
}

func (s *stubApp) SSHConfig(_ context.Context, options app.SSHConfigOptions) (string, error) {
	s.sshOptions = options
	return "Host main\n", nil
}

func (s *stubApp) Open(_ context.Context, name, port string, print bool) (string, error) {
	s.name = name
	s.argv = []string{port}
	if print {
		return "http://127.0.0.1:10100/", nil
	}
	return "", nil
}

func TestSSHConfigAndOpenFlags(t *testing.T) {
	service := &stubApp{}
	for flag, options := range map[string]app.SSHConfigOptions{
		"--include":     {Include: true},
		"--installer":   {Installer: true},
		"--known-hosts": {KnownHosts: true},
	} {
		_, _, err := execute(t, service, false, "", "ssh-config", flag)
		if err != nil || service.sshOptions != options {
			t.Fatalf("ssh-config %s = %+v, %v", flag, service.sshOptions, err)
		}
	}
	_, _, err := execute(t, service, false, "", "host", "init", "--ssh-include")
	if err != nil || !service.hostOptions.SSHInclude {
		t.Fatalf("host initialization = %+v, %v", service.hostOptions, err)
	}
	out, _, err := execute(t, service, false, "", "open", "main", "--print")
	if err != nil || service.name != "main" || !reflect.DeepEqual(service.argv, []string{"8000"}) || out != "http://127.0.0.1:10100/\n" {
		t.Fatalf("default open = %q, %v, %q, %v", service.name, service.argv, out, err)
	}
	out, _, err = execute(t, service, false, "", "open", "main", "9000")
	if err != nil || !reflect.DeepEqual(service.argv, []string{"9000"}) || out != "" {
		t.Fatalf("explicit open = %v, %q, %v", service.argv, out, err)
	}
}

func (s *stubApp) Logs(_ context.Context, name string, options app.LogOptions, emit func(app.LogLine) error) error {
	s.name, s.logOptions = name, options
	return emit(app.LogLine{Time: time.Unix(0, 0).UTC(), Source: "guest", Level: "info", Unit: "worker.service", Text: "ready"})
}

func TestLogsFlagsAndJSON(t *testing.T) {
	service := &stubApp{}
	out, _, err := execute(t, service, false, "", "logs", "main", "-f", "--source", "guest", "--unit", "worker.service", "-n", "12", "--json")
	want := app.LogOptions{Follow: true, Source: "guest", Unit: "worker.service", Lines: 12}
	if err != nil || service.name != "main" || service.logOptions != want {
		t.Fatalf("logs flags = %s %+v, %v", service.name, service.logOptions, err)
	}
	if out != `{"time":"1970-01-01T00:00:00Z","source":"guest","level":"info","unit":"worker.service","text":"ready"}`+"\n" {
		t.Fatalf("logs JSON = %q", out)
	}
	out, _, err = execute(t, service, false, "", "logs", "main")
	if err != nil || !strings.HasSuffix(out, " guest info worker.service: ready\n") || service.logOptions.Lines != 100 {
		t.Fatalf("plain logs = %q, %+v, %v", out, service.logOptions, err)
	}
}

func (*stubApp) List(context.Context) ([]app.Listing, error) {
	return []app.Listing{
		{Box: state.Box{Name: "main", Profile: "ubuntu", SSHPort: 2201, Forwards: state.DefaultForwards(1, false)}, Status: vm.StatusRunning},
		{Box: state.Box{Name: "secondary", Profile: "ubuntu", SSHPort: 2202}, Status: vm.StatusStopped},
	}, nil
}

func execute(t *testing.T, service *stubApp, terminal bool, input string, args ...string) (string, string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := newCommand(service, "v0.0.1-test", func(io.Reader) bool { return terminal })
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), stderr.String(), err
}

func TestNewAndHostFlags(t *testing.T) {
	service := &stubApp{}
	_, _, err := execute(t, service, false, "", "new", "main", "--profile", "ubuntu", "--cpus", "2", "--memory", "3", "--disk", "20")
	want := app.NewOptions{Name: "main", Profile: "ubuntu", CPUs: 2, MemoryGiB: 3, DiskGiB: 20}
	if err != nil || service.newOptions != want {
		t.Fatalf("new options = %+v, %v", service.newOptions, err)
	}
	_, _, err = execute(t, service, false, "", "host", "init", "--no-lan", "--authorized-key", "one.pub", "--authorized-key", "two.pub")
	if err != nil || service.hostOptions.LAN == nil || *service.hostOptions.LAN ||
		!reflect.DeepEqual(service.hostOptions.AuthorizedKeyFiles, []string{"one.pub", "two.pub"}) {
		t.Fatalf("host options = %+v, %v", service.hostOptions, err)
	}
	if _, _, err := execute(t, service, false, "", "host", "init", "--lan", "--no-lan"); err == nil {
		t.Fatal("accepted conflicting flags")
	}
}

func TestSSHAndPorts(t *testing.T) {
	service := &stubApp{}
	_, _, err := execute(t, service, false, "", "ssh", "main", "--", "printf", "-v", "hello world")
	if err != nil || service.name != "main" || !reflect.DeepEqual(service.argv, []string{"printf", "-v", "hello world"}) {
		t.Fatalf("ssh = %q %v, %v", service.name, service.argv, err)
	}
	out, _, err := execute(t, service, false, "", "ports", "main", "--set", "9000:30000", "--add", "9001:30001,9002:30002", "--rm", "9003")
	want := app.PortOptions{Set: []string{"9000:30000"}, Add: []string{"9001:30001", "9002:30002"}, Remove: []string{"9003"}}
	if err != nil || !reflect.DeepEqual(service.portOptions, want) || out != "GUEST  HOST   BIND\n9000   30000  127.0.0.1\n" {
		t.Fatalf("ports = %+v, %q, %v", service.portOptions, out, err)
	}
	out, _, err = execute(t, service, false, "", "ssh-config", "--host", "example.local")
	if err != nil || service.sshOptions.Host != "example.local" || out != "Host main\n" {
		t.Fatalf("ssh-config = %q %q, %v", service.sshOptions.Host, out, err)
	}
}

func TestRemovalConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		terminal    bool
		input       string
		force       bool
		wantErr     bool
		wantRemoval bool
	}{
		{name: "nonterminal", wantErr: true},
		{name: "force", force: true, wantRemoval: true},
		{name: "confirm", terminal: true, input: "yes\n", wantRemoval: true},
		{name: "empty", terminal: true, input: "\n"},
		{name: "decline", terminal: true, input: "no\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &stubApp{}
			args := []string{"rm", "main"}
			if tc.force {
				args = append(args, "-f")
			}
			_, stderr, err := execute(t, service, tc.terminal, tc.input, args...)
			if (err != nil) != tc.wantErr || (service.name == "main") != tc.wantRemoval {
				t.Fatalf("remove invoked for %q, error %v", service.name, err)
			}
			if tc.terminal && !strings.Contains(stderr, "[y/N]") {
				t.Fatalf("missing confirmation prompt: %q", stderr)
			}
		})
	}
}

func TestListAndVersionOutput(t *testing.T) {
	service := &stubApp{}
	out, _, err := execute(t, service, false, "", "ls")
	want := "NAME       STATUS   PROFILE  SSH   HTTP\n" +
		"main       running  ubuntu   2201  10100-10199\n" +
		"secondary  stopped  ubuntu   2202  -\n"
	if err != nil || out != want {
		t.Fatalf("listing = %q, %v", out, err)
	}
	out, _, err = execute(t, service, false, "", "version")
	if err != nil || out != "v0.0.1-test\n" {
		t.Fatalf("version = %q, %v", out, err)
	}
}

func TestRepinConfirmation(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal bool
		input    string
		yes      bool
		saves    bool
	}{
		{name: "requires --yes"},
		{name: "explicit flag", yes: true, saves: true},
		{name: "interactive approval", terminal: true, input: "yes\n", saves: true},
		{name: "interactive decline", terminal: true, input: "no\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &stubApp{}
			args := []string{"repin", "main"}
			if test.yes {
				args = append(args, "--yes")
			}
			_, output, err := execute(t, service, test.terminal, test.input, args...)
			if !test.terminal && !test.yes {
				if err == nil || !strings.Contains(err.Error(), "--yes") || service.name != "" {
					t.Fatalf("non-interactive repin: %q, %v", service.name, err)
				}
				return
			}
			if err != nil || (service.name == "main") != test.saves ||
				!strings.Contains(output, "SHA256:old") || !strings.Contains(output, "SHA256:new") ||
				strings.Contains(output, "ssh-config --installer") != test.saves {
				t.Fatalf("repin = %q, %q, %v", service.name, output, err)
			}
		})
	}
}
