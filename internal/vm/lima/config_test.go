package lima

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

var update = flag.Bool("update", false, "regenerate Lima YAML golden files")

func sampleSpec() vm.Spec {
	return vm.Spec{
		Name: "test-lima", Images: ubuntuImages, CPUs: 2, MemoryGiB: 2, DiskGiB: 10,
		User: "box", SSHPort: 2201,
		Forwards: []vm.PortForward{
			{GuestPort: 8000, HostPort: 10100, HostIP: "127.0.0.1"},
			{GuestPort: 8001, HostPort: 10101, HostIP: "0.0.0.0"},
		},
	}
}

func TestRenderConfig(t *testing.T) {
	for _, host := range []string{"darwin", "linux"} {
		t.Run(host, func(t *testing.T) {
			spec := sampleSpec()
			if host == "linux" {
				spec.DNS = []string{"1.1.1.1"}
				spec.Forwards = nil
			}
			actual, err := renderHostConfig(spec, host, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", host+".golden.yaml")
			if *update {
				if err := os.WriteFile(path, actual, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, expected) {
				t.Fatalf("config differs from %s:\n%s", path, actual)
			}
		})
	}
	if _, err := renderConfig(sampleSpec(), "windows"); err == nil {
		t.Fatal("renderConfig accepted unsupported Windows host")
	}
}

func TestValidateConfig(t *testing.T) {
	limactl, err := exec.LookPath("limactl")
	if err != nil {
		t.Skip("limactl unavailable")
	}
	for _, host := range []string{"darwin", "linux"} {
		t.Run(host, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.yaml")
			data, err := renderConfig(sampleSpec(), host)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), limactl, "validate", "--fill", path)
			cmd.Env = append(os.Environ(), "LIMA_HOME="+t.TempDir())
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("limactl validate: %v\n%s", err, output)
			}
		})
	}
}

func TestSocketNetworkDisablesBuiltInHostServices(t *testing.T) {
	spec := sampleSpec()
	spec.HostGateway = true
	spec.Socket = vm.SocketAttachment{
		Path: "/fixture/network.sock", Interface: "lima0", MAC: "52:55:0a:72:00:15", RouteMetric: 100,
	}
	config, err := renderConfig(spec, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"socket: /fixture/network.sock", "interface: lima0", "metric: 100", "hostLoopback: false", "enabled: false"} {
		if !bytes.Contains(config, []byte(expected)) {
			t.Errorf("config missing %q", expected)
		}
	}
}

func TestNestedVirtConfig(t *testing.T) {
	spec := sampleSpec()
	spec.NestedVirt = true
	data, err := renderHostConfig(spec, "linux", "amd64")
	if err != nil || !bytes.Contains(data, []byte("x86_64: host\n")) {
		t.Fatalf("expected host CPU for opted-in VM, got %v\n%s", err, data)
	}
	for _, host := range [][2]string{{"darwin", "arm64"}, {"linux", "arm64"}, {"windows", "amd64"}} {
		if _, err := renderHostConfig(spec, host[0], host[1]); err == nil {
			t.Errorf("accepted nested virtualization on %s/%s", host[0], host[1])
		}
	}
}
