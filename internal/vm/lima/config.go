package lima

import (
	"crypto/rand"
	"fmt"

	"github.com/EmmadUsmani/homebox/internal/vm"
	"go.yaml.in/yaml/v3"
)

type config struct {
	Probes     []probeConfig     `yaml:"probes,omitempty"`
	Provision  []provisionConfig `yaml:"provision,omitempty"`
	Param      map[string]string `yaml:"param,omitempty"`
	VMType     string            `yaml:"vmType"`
	Images     []image           `yaml:"images"`
	CPUs       int               `yaml:"cpus"`
	Memory     string            `yaml:"memory"`
	Disk       string            `yaml:"disk"`
	Plain      bool              `yaml:"plain"`
	Mounts     []string          `yaml:"mounts"`
	SSH        sshConfig         `yaml:"ssh"`
	Containerd struct {
		System bool `yaml:"system"`
		User   bool `yaml:"user"`
	} `yaml:"containerd"`
	PropagateProxyEnv bool            `yaml:"propagateProxyEnv"`
	NetworkOpts       *networkOptions `yaml:"networkOpts,omitempty"`
	User              userConfig      `yaml:"user"`
	PortForwards      []portRule      `yaml:"portForwards"`
	HostResolver      *resolverConfig `yaml:"hostResolver,omitempty"`
	Networks          []networkConfig `yaml:"networks,omitempty"`
	DNS               []string        `yaml:"dns,omitempty"`
}

type networkOptions struct {
	HostLoopback bool `yaml:"hostLoopback"`
}

type probeConfig struct {
	Mode        string `yaml:"mode"`
	Description string `yaml:"description"`
	Script      string `yaml:"script"`
}

type provisionConfig struct {
	Mode   string `yaml:"mode"`
	Script string `yaml:"script"`
}

type networkConfig struct {
	Socket    string `yaml:"socket" json:"socket"`
	Interface string `yaml:"interface" json:"interface"`
	MAC       string `yaml:"macAddress" json:"macAddress"`
	Metric    int    `yaml:"metric" json:"metric"`
}

func socketNetwork(socket vm.SocketAttachment) networkConfig {
	return networkConfig{Socket: socket.Path, Interface: socket.Interface, MAC: socket.MAC, Metric: socket.RouteMetric}
}

type image struct {
	Location string `yaml:"location"`
	Arch     string `yaml:"arch"`
	Digest   string `yaml:"digest,omitempty"`
}

type sshConfig struct {
	LocalPort         int  `yaml:"localPort"`
	ForwardAgent      bool `yaml:"forwardAgent"`
	ForwardX11        bool `yaml:"forwardX11"`
	LoadDotSSHPubKeys bool `yaml:"loadDotSSHPubKeys"`
}

type userConfig struct {
	Name    string `yaml:"name"`
	Home    string `yaml:"home"`
	Comment string `yaml:"comment"`
}

type resolverConfig struct {
	Enabled bool `yaml:"enabled"`
}

type portRule struct {
	GuestPort int    `yaml:"guestPort,omitempty" json:"guestPort,omitempty"`
	HostPort  int    `yaml:"hostPort,omitempty" json:"hostPort,omitempty"`
	HostIP    string `yaml:"hostIP,omitempty" json:"hostIP,omitempty"`
	GuestIP   string `yaml:"guestIP,omitempty" json:"guestIP,omitempty"`
	Proto     string `yaml:"proto" json:"proto"`
	Static    bool   `yaml:"static,omitempty" json:"static,omitempty"`
	Ignore    bool   `yaml:"ignore,omitempty" json:"ignore,omitempty"`
}

func backend(goos string) string {
	switch goos {
	case "darwin":
		return "vz"
	case "linux":
		return "qemu"
	default:
		return ""
	}
}

func renderConfig(spec vm.Spec, goos string) ([]byte, error) {
	vmType := backend(goos)
	if vmType == "" {
		return nil, fmt.Errorf("unsupported Lima host OS %q", goos)
	}
	settings := config{
		VMType:       vmType,
		Images:       make([]image, 0, len(spec.Images)),
		CPUs:         spec.CPUs,
		Memory:       fmt.Sprintf("%dGiB", spec.MemoryGiB),
		Disk:         fmt.Sprintf("%dGiB", spec.DiskGiB),
		Plain:        true,
		SSH:          sshConfig{LocalPort: spec.SSHPort},
		User:         userConfig{Name: spec.User, Home: "/home/" + spec.User, Comment: spec.User},
		PortForwards: portRules(spec.Forwards),
	}
	if spec.ImageDisk != "" {
		identity := rand.Text()
		settings.Provision = []provisionConfig{{Mode: "boot", Script: imageBootScript(identity, spec.User)}}
		settings.Probes = []probeConfig{{
			Mode: "readiness", Description: "the saved disk has a fresh guest identity",
			Script: fmt.Sprintf("#!/bin/sh\n[ \"$(cat /etc/homebox-image-id)\" = %s ]\n", identity),
		}}
	}
	if goos == "linux" {
		settings.Param = map[string]string{"internal_netplanOptional": "true"}
	}
	if goos == "darwin" {
		settings.NetworkOpts = &networkOptions{HostLoopback: spec.HostGateway && spec.Socket.Path == ""}
	}
	for _, source := range spec.Images {
		settings.Images = append(settings.Images, image{Location: source.URL, Arch: source.Arch, Digest: source.Digest})
	}
	if len(spec.DNS) > 0 {
		settings.HostResolver = &resolverConfig{Enabled: false}
		settings.DNS = spec.DNS
	}
	if spec.Socket.Path != "" {
		settings.Networks = []networkConfig{socketNetwork(spec.Socket)}
		settings.HostResolver = &resolverConfig{Enabled: false}
	}
	return yaml.Marshal(settings)
}

func portRules(forwards []vm.PortForward) []portRule {
	rules := make([]portRule, 0, len(forwards)+3)
	// Plain mode retains static TCP forwards and disables listener discovery.
	for _, forward := range forwards {
		rules = append(rules, portRule{
			GuestPort: forward.GuestPort, HostPort: forward.HostPort, HostIP: forward.HostIP,
			Proto: "tcp", Static: true,
		})
	}
	for _, ip := range []string{"0.0.0.0", "127.0.0.1", "::"} {
		rules = append(rules, portRule{GuestIP: ip, Proto: "any", Ignore: true})
	}
	return rules
}
