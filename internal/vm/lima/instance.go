package lima

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

// hostAddress is where Lima's user-mode network maps the host's loopback.
const hostAddress = "192.168.5.2"

type instanceRecord struct {
	Name         string `json:"name"`
	Dir          string `json:"dir"`
	Status       string `json:"status"`
	CPUs         int    `json:"cpus"`
	Memory       int64  `json:"memory"`
	Disk         int64  `json:"disk"`
	SSHLocalPort int    `json:"sshLocalPort"`
	Config       struct {
		Networks []networkConfig `json:"networks"`
		User     struct {
			Name string `json:"name"`
		} `json:"user"`
	} `json:"config"`
}

func (r instanceRecord) instance() vm.Instance {
	status := vm.StatusBroken
	switch r.Status {
	case "Running":
		status = vm.StatusRunning
	case "Stopped", "Uninitialized":
		status = vm.StatusStopped
	}
	instance := vm.Instance{
		Name: r.Name, Status: status, RuntimeDir: r.Dir, HostAddress: hostAddress,
		HostPIDFile: filepath.Join(r.Dir, "ha.pid"),
	}
	const gib = 1 << 30
	if r.Memory%gib == 0 && r.Disk%gib == 0 {
		instance.Size = vm.Size{CPUs: r.CPUs, MemoryGiB: int(r.Memory / gib), DiskGiB: int(r.Disk / gib)}
	}
	if r.Dir != "" {
		instance.HostLog = filepath.Join(r.Dir, "ha.stderr.log")
	}
	for _, network := range r.Config.Networks {
		if network.Socket != "" {
			instance.Socket = vm.SocketAttachment{Path: network.Socket, Interface: network.Interface, MAC: network.MAC, RouteMetric: network.Metric}
			break
		}
	}
	return instance
}

// List reports all instances in the driver's Lima home.
func (d *Driver) List(ctx context.Context) ([]vm.Instance, error) {
	records, err := d.records(ctx)
	if err != nil {
		return nil, err
	}
	instances := make([]vm.Instance, 0, len(records))
	for _, record := range records {
		instances = append(instances, record.instance())
	}
	return instances, nil
}

// Get reports the state of a named VM.
func (d *Driver) Get(ctx context.Context, name string) (vm.Instance, error) {
	record, err := d.find(ctx, name)
	if err != nil {
		return vm.Instance{}, err
	}
	return record.instance(), nil
}

// SSH reports the loopback SSH endpoint and Lima-managed key for a running VM.
func (d *Driver) SSH(ctx context.Context, name string) (vm.Endpoint, error) {
	record, err := d.find(ctx, name)
	if err != nil {
		return vm.Endpoint{}, err
	}
	if record.instance().Status != vm.StatusRunning || record.SSHLocalPort <= 0 || record.Config.User.Name == "" {
		return vm.Endpoint{}, fmt.Errorf("Lima instance %q has no running SSH endpoint", name)
	}
	return vm.Endpoint{
		Host: "127.0.0.1", Port: record.SSHLocalPort, User: record.Config.User.Name,
		IdentityFile: filepath.Join(d.options.Home, "_config", "user"),
		KnownHosts:   filepath.Join(record.Dir, "ssh.known_hosts"),
	}, nil
}

func (d *Driver) find(ctx context.Context, name string) (instanceRecord, error) {
	if err := validateName(name); err != nil {
		return instanceRecord{}, err
	}
	records, err := d.records(ctx)
	if err != nil {
		return instanceRecord{}, err
	}
	for _, record := range records {
		if record.Name == name {
			return record, nil
		}
	}
	return instanceRecord{}, fmt.Errorf("Lima instance %q: %w", name, vm.ErrNotFound)
}

func (d *Driver) records(ctx context.Context) ([]instanceRecord, error) {
	result, err := d.run(ctx, nil, "list", "--json")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Stdout))
	var records []instanceRecord
	for {
		var record instanceRecord
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				return records, nil
			}
			return nil, fmt.Errorf("decode limactl list output: %w", err)
		}
		if record.Name == "" {
			return nil, fmt.Errorf("decode limactl list output: instance name is empty")
		}
		records = append(records, record)
	}
}
