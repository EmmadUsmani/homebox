// Package lima implements the VM driver using limactl.
package lima

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Options configures the Lima command and its isolated state directory.
type Options struct {
	Home    string       // Required LIMA_HOME.
	Limactl string       // Defaults to limactl on PATH.
	Runner  execx.Runner // Defaults to execx.OS.
}

// Driver manages VMs in a dedicated Lima home.
type Driver struct {
	options Options
}

var _ vm.Driver = (*Driver)(nil)

// New returns a driver for the given Lima home.
func New(opts Options) *Driver {
	if opts.Limactl == "" {
		opts.Limactl = "limactl"
	}
	if opts.Runner == nil {
		opts.Runner = execx.OS{}
	}
	return &Driver{options: opts}
}

// Name identifies the driver.
func (*Driver) Name() string { return "lima" }

// CheckNestedVirt reports whether the host supports nested KVM.
func (*Driver) CheckNestedVirt() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("--nested-virt requires Lima QEMU/KVM on an x86-64 Linux host")
	}
	for _, module := range []string{"kvm_intel", "kvm_amd"} {
		value, err := os.ReadFile(filepath.Join("/sys/module", module, "parameters/nested"))
		if err == nil {
			switch strings.TrimSpace(string(value)) {
			case "Y", "1":
				return nil
			}
		}
	}
	return errors.New("--nested-virt requires nested KVM enabled in the host kvm_intel or kvm_amd module")
}

// Create creates a stopped VM from the supplied spec.
func (d *Driver) Create(ctx context.Context, spec vm.Spec) error {
	if err := validateName(spec.Name); err != nil {
		return err
	}
	if spec.ImageDisk != "" && runtime.GOOS != "darwin" {
		return errors.New("creating a VM from a saved image requires Lima with VZ on macOS")
	}
	data, err := renderConfig(spec, runtime.GOOS)
	if err != nil {
		return fmt.Errorf("render Lima config for %q: %w", spec.Name, err)
	}
	if spec.ImageDisk != "" {
		return d.createFromImage(ctx, spec, data)
	}
	dir, err := os.MkdirTemp("", "homebox-lima-")
	if err != nil {
		return fmt.Errorf("prepare Lima config for %q: %w", spec.Name, err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "instance.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write Lima config for %q: %w", spec.Name, err)
	}
	_, err = d.run(ctx, nil, "create", "--tty=false", "--name", spec.Name, path)
	return err
}

// Start starts an existing VM under the optional command prefix.
func (d *Driver) Start(ctx context.Context, name string, opts vm.StartOptions) error {
	// Lima treats a missing bare instance name as a request to create from its default template.
	record, err := d.find(ctx, name)
	if err != nil {
		return err
	}
	instance := record.instance()
	if opts.NestedVirt && instance.Status != vm.StatusRunning {
		if err := d.CheckNestedVirt(); err != nil {
			return err
		}
	}
	var changes []string
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && record.VMType == "qemu" && instance.Status != vm.StatusRunning {
		cpu := qemuCPUType(opts.NestedVirt)
		if record.Config.VMOpts.QEMU == nil || record.Config.VMOpts.QEMU.CPUType["x86_64"] != cpu {
			changes = append(changes, fmt.Sprintf(".vmOpts.qemu.cpuType.x86_64 = %q", cpu))
		}
	}
	if opts.Socket.Path != "" && instance.Socket != opts.Socket && instance.Status != vm.StatusRunning {
		networks, err := json.Marshal([]networkConfig{socketNetwork(opts.Socket)})
		if err != nil {
			return err
		}
		changes = append(changes, ".networks = "+string(networks), ".hostResolver.enabled = false")
		if runtime.GOOS == "darwin" {
			changes = append(changes, ".networkOpts.hostLoopback = false")
		}
		if len(opts.DNS) > 0 {
			resolvers, err := json.Marshal(opts.DNS)
			if err != nil {
				return err
			}
			changes = append(changes, ".dns = "+string(resolvers))
		}
	}
	if len(changes) > 0 {
		if _, err := d.run(ctx, nil, "edit", "--tty=false", "--set", strings.Join(changes, " | "), name); err != nil {
			return err
		}
	}
	if runtime.GOOS == "linux" && opts.Socket.Path != "" {
		// Lima initializes networks.yaml lazily. Keep that write outside the
		// long-lived hostagent's otherwise read-only shared configuration.
		if _, err := d.run(ctx, nil, "network", "list", "--json"); err != nil {
			return err
		}
	}
	_, err = d.run(ctx, opts.Wrap, "start", "--tty=false", name)
	return err
}

// Stop shuts down a VM.
func (d *Driver) Stop(ctx context.Context, name string) error {
	if _, err := d.Get(ctx, name); err != nil {
		return err
	}
	_, err := d.run(ctx, nil, "stop", name)
	return err
}

// Delete removes a VM and its disks, accepting an already absent VM.
func (d *Driver) Delete(ctx context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	_, err := d.run(ctx, nil, "delete", "--force", name)
	if errors.Is(err, vm.ErrNotFound) {
		return nil
	}
	return err
}

// SetForwards updates the forwarding rules in a stopped VM's configuration.
func (d *Driver) SetForwards(ctx context.Context, name string, forwards []vm.PortForward) error {
	instance, err := d.Get(ctx, name)
	if err != nil {
		return err
	}
	if instance.Status != vm.StatusStopped {
		return fmt.Errorf("edit Lima forwards for %q: VM must be stopped (status %s)", name, instance.Status)
	}
	rules, err := json.Marshal(portRules(forwards))
	if err != nil {
		return fmt.Errorf("encode Lima forwards for %q: %w", name, err)
	}
	_, err = d.run(ctx, nil, "edit", "--tty=false", "--set", ".portForwards = "+string(rules), name)
	return err
}

func (d *Driver) run(ctx context.Context, wrap []string, args ...string) (execx.Result, error) {
	if d.options.Home == "" {
		return execx.Result{}, errors.New("Lima home is required")
	}
	command := execx.Cmd{Name: d.options.Limactl, Args: args, Env: []string{"LIMA_HOME=" + d.options.Home}}
	if len(wrap) > 0 {
		command.Name = wrap[0]
		command.Args = append(append(slices.Clone(wrap[1:]), d.options.Limactl), args...)
	}
	result, err := d.options.Runner.Run(ctx, command)
	if err != nil {
		if len(args) > 1 {
			name := args[len(args)-1]
			stderr := string(result.Stderr)
			if args[0] == "stop" && strings.Contains(stderr, "/"+name+"/lima.yaml: no such file or directory") ||
				(args[0] == "edit" || args[0] == "delete") && strings.Contains(stderr, fmt.Sprintf("instance %q not found", name)) {
				err = errors.Join(vm.ErrNotFound, err)
			}
		}
		return result, fmt.Errorf("limactl %s: %w", args[0], err)
	}
	return result, nil
}

var instanceName = regexp.MustCompile(`^[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)*$`)

func validateName(name string) error {
	lower := strings.ToLower(name)
	if len(name) > 76 || !instanceName.MatchString(name) ||
		strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return fmt.Errorf("invalid Lima instance name %q", name)
	}
	return nil
}
