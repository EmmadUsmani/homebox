// Package vmtest provides an in-memory VM driver for tests.
package vmtest

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Call records one driver operation.
type Call struct {
	Method   string
	Name     string
	Spec     vm.Spec
	Start    vm.StartOptions
	Forwards []vm.PortForward
}

// Driver records operations and models VM status. Configure fields before use.
type Driver struct {
	mu        sync.Mutex
	Instances map[string]vm.Instance
	Specs     map[string]vm.Spec
	Errors    map[string]error
	Calls     []Call
}

var _ vm.Driver = (*Driver)(nil)

// Name identifies the fake driver.
func (*Driver) Name() string { return "fake" }

func (d *Driver) record(call Call) error {
	d.Calls = append(d.Calls, call)
	return d.Errors[call.Method]
}

// Create records a stopped instance and its spec.
func (d *Driver) Create(_ context.Context, spec vm.Spec) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(Call{Method: "Create", Name: spec.Name, Spec: spec}); err != nil {
		return err
	}
	if d.Instances == nil {
		d.Instances = make(map[string]vm.Instance)
	}
	if d.Specs == nil {
		d.Specs = make(map[string]vm.Spec)
	}
	d.Instances[spec.Name] = vm.Instance{Name: spec.Name, Status: vm.StatusStopped}
	d.Specs[spec.Name] = spec
	return nil
}

// Start updates the instance to running.
func (d *Driver) Start(_ context.Context, name string, opts vm.StartOptions) error {
	return d.setStatus(Call{Method: "Start", Name: name, Start: opts}, vm.StatusRunning)
}

// Stop updates the instance to stopped.
func (d *Driver) Stop(_ context.Context, name string) error {
	return d.setStatus(Call{Method: "Stop", Name: name}, vm.StatusStopped)
}

func (d *Driver) setStatus(call Call, status vm.Status) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(call); err != nil {
		return err
	}
	instance, ok := d.Instances[call.Name]
	if !ok {
		return vm.ErrNotFound
	}
	instance.Status = status
	d.Instances[call.Name] = instance
	return nil
}

// Delete removes an instance and its spec.
func (d *Driver) Delete(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(Call{Method: "Delete", Name: name}); err != nil {
		return err
	}
	delete(d.Instances, name)
	delete(d.Specs, name)
	return nil
}

// Get returns one instance by name.
func (d *Driver) Get(_ context.Context, name string) (vm.Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(Call{Method: "Get", Name: name}); err != nil {
		return vm.Instance{}, err
	}
	instance, ok := d.Instances[name]
	if !ok {
		return vm.Instance{}, vm.ErrNotFound
	}
	return instance, nil
}

// List returns all instances in name order.
func (d *Driver) List(_ context.Context) ([]vm.Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(Call{Method: "List"}); err != nil {
		return nil, err
	}
	var instances []vm.Instance
	for _, instance := range d.Instances {
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return instances, nil
}

// SSH returns a loopback endpoint for a running instance.
func (d *Driver) SSH(_ context.Context, name string) (vm.Endpoint, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(Call{Method: "SSH", Name: name}); err != nil {
		return vm.Endpoint{}, err
	}
	if d.Instances[name].Status != vm.StatusRunning {
		return vm.Endpoint{}, vm.ErrNotFound
	}
	spec := d.Specs[name]
	return vm.Endpoint{Host: "127.0.0.1", Port: spec.SSHPort, User: spec.User}, nil
}

// SetForwards replaces an instance's port forwards.
func (d *Driver) SetForwards(_ context.Context, name string, forwards []vm.PortForward) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.record(Call{Method: "SetForwards", Name: name, Forwards: slices.Clone(forwards)}); err != nil {
		return err
	}
	spec, ok := d.Specs[name]
	if !ok {
		return vm.ErrNotFound
	}
	spec.Forwards = slices.Clone(forwards)
	d.Specs[name] = spec
	return nil
}
