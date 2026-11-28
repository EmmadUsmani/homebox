package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/EmmadUsmani/homebox/internal/hostinfo"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

type VMStatus struct {
	Name          string              `json:"name"`
	NestedVirt    bool                `json:"nested_virt"`
	State         string              `json:"state"`
	Profile       string              `json:"profile,omitempty"`
	SSHPort       int                 `json:"ssh_port,omitempty"`
	Forwards      []vm.PortForward    `json:"forwards,omitempty"`
	Provisioning  *ProvisionState     `json:"provisioning,omitempty"`
	UptimeSeconds *uint64             `json:"uptime_seconds,omitempty"`
	Host          *hostinfo.Usage     `json:"host,omitempty"`
	HostAgent     *hostinfo.Usage     `json:"hostagent,omitempty"`
	Guest         *GuestStatus        `json:"guest,omitempty"`
	Ports         []PortStatus        `json:"ports,omitempty"`
	Containers    []ContainerStatus   `json:"containers,omitempty"`
	Agents        []AgentStatus       `json:"agents,omitempty"`
	Denials       []NetDenial         `json:"denials,omitempty"`
	Doctor        *state.DoctorResult `json:"doctor,omitempty"`
	Errors        []string            `json:"errors,omitempty"`
}

type ProvisionState struct {
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Step       string     `json:"step,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type GuestStatus struct {
	UptimeSeconds *uint64         `json:"uptime_seconds,omitempty"`
	Load          *[3]float64     `json:"load,omitempty"`
	Memory        *StorageUsage   `json:"memory,omitempty"`
	Disk          *StorageUsage   `json:"disk,omitempty"`
	Processes     []ProcessStatus `json:"processes,omitempty"`
}

type StorageUsage struct {
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

type ProcessStatus struct {
	PID           int     `json:"pid"`
	Name          string  `json:"name"`
	CPUPercent    float64 `json:"cpu_percent"`
	ResidentBytes uint64  `json:"resident_bytes"`
}

type PortStatus struct {
	Address   string            `json:"address"`
	GuestPort int               `json:"guest_port"`
	Processes []ListenerProcess `json:"processes,omitempty"`
	Published bool              `json:"published"`
	HostPort  int               `json:"host_port,omitempty"`
	HostURL   string            `json:"host_url,omitempty"`
}

type ListenerProcess struct {
	Name string `json:"name"`
	PID  int    `json:"pid"`
}

type ContainerStatus struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type AgentStatus struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	CWD  string `json:"cwd,omitempty"`
}

type TopSnapshot struct {
	At  time.Time  `json:"at"`
	VMs []VMStatus `json:"vms"`
}

// Status returns the host record and the readings available for one VM.
func (s *Service) Status(ctx context.Context, name string) (VMStatus, error) {
	box, err := s.load(name)
	if err != nil {
		return VMStatus{}, err
	}
	instance, err := s.Driver.Get(ctx, name)
	if err != nil && !errors.Is(err, vm.ErrNotFound) {
		return VMStatus{}, err
	}
	if instance.Status == "" {
		instance.Status = vm.StatusBroken
	}
	status := s.localStatus(Listing{Box: box, Status: instance.Status})
	if instance.Status == vm.StatusRunning {
		status.setHostUsage(s.sampleHost(ctx, []vm.Instance{instance})[name])
	}
	status.Denials, err = readDenials(state.NetworkLogPath(s.Store.RunDir(name)))
	status.addError("network log", err)
	if instance.Status == vm.StatusRunning {
		s.probeGuest(ctx, &status, true)
	}
	return status, nil
}

// ListStatus returns the host-side facts without opening any guest SSH sessions.
func (s *Service) ListStatus(ctx context.Context) ([]VMStatus, error) {
	boxes, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	host := s.sampleHost(ctx, runningInstances(boxes))
	statuses := make([]VMStatus, 0, len(boxes))
	for _, box := range boxes {
		status := s.localStatus(box)
		status.setHostUsage(host[box.Name])
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Top samples each selected running VM concurrently.
func (s *Service) Top(ctx context.Context, name string) (TopSnapshot, error) {
	if name != "" {
		if _, err := s.load(name); err != nil {
			return TopSnapshot{}, err
		}
	}
	boxes, err := s.List(ctx)
	if err != nil {
		return TopSnapshot{}, err
	}
	snapshot := TopSnapshot{At: time.Now().UTC()}
	selected := make([]Listing, 0, len(boxes))
	for _, box := range boxes {
		if box.Status == vm.StatusRunning && (name == "" || box.Name == name) {
			selected = append(selected, box)
		}
	}
	snapshot.VMs = make([]VMStatus, len(selected))
	var probes sync.WaitGroup
	var host map[string]hostinfo.UsageSample
	probes.Go(func() { host = s.sampleHost(ctx, runningInstances(selected)) })
	for index, box := range selected {
		probes.Go(func() {
			probeCtx, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
			defer cancel()
			status := VMStatus{Name: box.Name, NestedVirt: box.NestedVirt, State: string(box.Status)}
			s.probeGuest(probeCtx, &status, false)
			snapshot.VMs[index] = status
		})
	}
	probes.Wait()
	for index := range snapshot.VMs {
		status := &snapshot.VMs[index]
		status.setHostUsage(host[status.Name])
	}
	return snapshot, nil
}

func (s *Service) localStatus(box Listing) VMStatus {
	status := VMStatus{
		Name: box.Name, NestedVirt: box.NestedVirt, State: string(box.Status), Profile: box.Profile,
		SSHPort: box.SSHPort, Forwards: box.Forwards,
	}
	run, recorded, err := s.Store.LoadProvisionRun(box.Name)
	status.addError("provisioning record", err)
	if recorded {
		status.State = provisionStatus(status.State, run, hostinfo.ProcessAlive)
		status.Provisioning = &ProvisionState{
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Error: untrusted.Line(run.Error),
		}
		if run.FinishedAt == nil || run.Error != "" {
			status.Provisioning.Step = untrusted.Line(run.Step)
		}
	}
	if box.Status == vm.StatusRunning {
		status.addError("network", s.networkDaemonError(box.Name))
	}
	status.Doctor, err = s.Store.LoadDoctorResult(box.Name)
	status.addError("doctor record", err)
	return status
}

func provisionStatus(current string, run state.ProvisionRun, alive func(int) bool) string {
	switch {
	case run.Error != "":
		return "provision-failed"
	case run.FinishedAt != nil:
		return current
	case alive(run.PID):
		return "provisioning"
	default:
		return "provision-interrupted"
	}
}

func runningInstances(boxes []Listing) []vm.Instance {
	instances := make([]vm.Instance, 0, len(boxes))
	for _, box := range boxes {
		if box.Status == vm.StatusRunning {
			instances = append(instances, box.instance)
		}
	}
	return instances
}

func (s *Service) sampleHost(ctx context.Context, instances []vm.Instance) map[string]hostinfo.UsageSample {
	if s.HostUsage == nil {
		return nil
	}
	return s.HostUsage.Sample(ctx, instances)
}

func (status *VMStatus) setHostUsage(sample hostinfo.UsageSample) {
	status.Host = sample.VM
	status.HostAgent = sample.HostAgent
	if status.Host == nil {
		status.Host = sample.HostAgent
		status.HostAgent = nil
	}
	if status.UptimeSeconds == nil && status.Host != nil {
		status.UptimeSeconds = &status.Host.UptimeSeconds
	}
}

func (status *VMStatus) addError(source string, err error) {
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		status.Errors = append(status.Errors, fmt.Sprintf("%s: %s", source, untrusted.Line(err.Error())))
	}
}

func matchPorts(ports []PortStatus, forwards []vm.PortForward) {
	published := make(map[int]int, len(forwards))
	for _, forward := range forwards {
		published[forward.GuestPort] = forward.HostPort
	}
	for index := range ports {
		port := &ports[index]
		if hostPort, ok := published[port.GuestPort]; ok {
			port.Published = true
			port.HostPort = hostPort
			port.HostURL = publishedURL(hostPort)
		}
	}
}
