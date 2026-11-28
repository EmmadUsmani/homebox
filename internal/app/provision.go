package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/EmmadUsmani/homebox/internal/guest"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/EmmadUsmani/homebox/internal/vmnet"
)

var (
	guestUser       = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,30}$`)
	invalidUserRune = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// NewOptions overrides the default profile or VM size.
type NewOptions struct {
	ForceProvision bool
	Version        string
	Name           string
	Profile        string
	Image          string
	CPUs           int
	MemoryGiB      int
	DiskGiB        int
}

// NewDefaults returns the defaults used when creating a VM on this host.
func (s *Service) NewDefaults() (NewOptions, error) {
	var options NewOptions
	err := s.resolveNewOptions(&options)
	return options, err
}

// ProvisionError identifies a failure after the VM record was reserved.
type ProvisionError struct {
	Name string
	Err  error
}

func (e *ProvisionError) Error() string {
	return fmt.Sprintf("provision %s: %v; retry with box provision %s or remove with box rm %s",
		e.Name, e.Err, e.Name, e.Name)
}

func (e *ProvisionError) Unwrap() error { return e.Err }

func defaultUser() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("determine current host user: %w", err)
	}
	_, name, hasDomain := strings.Cut(current.Username, "\\")
	if !hasDomain {
		name = current.Username
	}
	name = strings.Trim(invalidUserRune.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if name == "" || name == "root" {
		return "homebox", nil
	}
	if !guestUser.MatchString(name[:1]) {
		name = "u-" + name
	}
	return name[:min(len(name), 31)], nil
}

func (s *Service) validateSocketPaths(name string) error {
	type validator interface{ ValidateSocketPaths(string) error }
	for _, component := range []any{s.Driver, s.Isolation} {
		if checker, ok := component.(validator); ok {
			if err := checker.ValidateSocketPaths(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// New reserves a box record, creates its VM and provisions the guest.
func (s *Service) New(ctx context.Context, options NewOptions) error {
	if err := state.ValidateName(options.Name); err != nil {
		return err
	}
	if err := s.validateSocketPaths(options.Name); err != nil {
		return err
	}
	if options.CPUs < 0 || options.MemoryGiB < 0 || options.DiskGiB < 0 {
		return errors.New("CPU, memory and disk sizes must be positive")
	}
	username, err := defaultUser()
	if err != nil {
		return err
	}
	if options.ForceProvision && options.Image != "" {
		return errors.New("--force-provision cannot be used with --image")
	}
	return s.Store.WithLock(func() error {
		if options.Image != "" {
			if err := s.resolveImageOptions(&options); err != nil {
				return err
			}
		}
		if err := s.resolveNewOptions(&options); err != nil {
			return err
		}
		profile, err := s.Profiles.Get(options.Profile)
		if err != nil {
			return err
		}
		if profile.OS == vm.DarwinGuest && (runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" || options.Image != "") {
			return errors.New("macOS guests require an Apple silicon Mac and an installer image")
		}
		return s.newBox(ctx, options, profile, username, false)
	})
}

func (s *Service) newBox(ctx context.Context, options NewOptions, profile guest.Profile, username string, build bool) error {
	if _, err := s.Store.Load(options.Name); err == nil {
		return fmt.Errorf("box %q already exists", options.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := s.Driver.Get(ctx, options.Name); err == nil {
		return fmt.Errorf("driver already has an instance named %q", options.Name)
	} else if !errors.Is(err, vm.ErrNotFound) {
		return err
	}
	host, err := s.Store.LoadHost()
	if err != nil {
		return err
	}
	baseImage := false
	if !options.ForceProvision && options.Image == "" {
		options.Image, err = s.profileBase(ctx, options, profile, username)
		if err != nil {
			return err
		}
		baseImage = options.Image != ""
	}
	index, err := s.Store.NextIndex()
	if err != nil {
		return err
	}
	box := state.Box{
		Name: options.Name, Image: options.Image, BaseImage: baseImage, ImageBuild: build, Profile: options.Profile, Driver: s.Driver.Name(), Index: index,
		CPUs: options.CPUs, MemoryGiB: options.MemoryGiB,
		DiskGiB: options.DiskGiB, User: username, SSHPort: state.SSHPort(index),
		Forwards: state.DefaultForwards(index, host.LAN && !build), CreatedAt: time.Now().UTC(),
	}
	if err := s.validatePorts(box); err != nil {
		return err
	}
	if err := s.Store.Save(box); err != nil {
		return err
	}
	return s.provision(ctx, box, profile)
}

func (s *Service) resolveNewOptions(options *NewOptions) error {
	defaults, err := s.checkHostSize(options.CPUs, options.MemoryGiB)
	if err != nil {
		return err
	}
	options.CPUs = sizeOrDefault(options.CPUs, defaults.CPUs)
	options.MemoryGiB = sizeOrDefault(options.MemoryGiB, defaults.MemoryGiB)
	options.DiskGiB = sizeOrDefault(options.DiskGiB, 50)
	if options.Profile == "" {
		options.Profile = guest.DefaultProfile
	}
	return nil
}

func (s *Service) checkHostSize(requestCPUs, requestMemoryGiB int) (vm.Size, error) {
	const bytesPerGiB = 1 << 30
	cpus, memory, err := s.HostCapacity.Capacity()
	if err != nil {
		return vm.Size{}, fmt.Errorf("determine host capacity: %w", err)
	}
	if requestCPUs > cpus {
		return vm.Size{}, fmt.Errorf("--cpus %d exceeds the host's %d CPUs", requestCPUs, cpus)
	}
	defaults := vm.Size{CPUs: min(4, cpus), MemoryGiB: 8}
	if memory == 0 {
		return defaults, nil
	}
	hostGiB := int(memory / bytesPerGiB)
	maxExplicitGiB := max(0, hostGiB-2)
	if requestMemoryGiB > maxExplicitGiB {
		return vm.Size{}, fmt.Errorf("--memory %d GiB exceeds the host limit of %d GiB (%g GiB RAM on the host, with 2 GiB reserved)",
			requestMemoryGiB, maxExplicitGiB, float64(memory)/bytesPerGiB)
	}
	defaults.MemoryGiB = max(2, min(8, hostGiB/2))
	return defaults, nil
}

func sizeOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// Provision runs all the profile steps and can retry a partially created box.
func (s *Service) Provision(ctx context.Context, name string) error {
	return s.Store.WithLock(func() error {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		profile, err := s.Profiles.Get(box.Profile)
		if err != nil {
			return err
		}
		return s.provision(ctx, box, profile)
	})
}

func (s *Service) provision(ctx context.Context, box state.Box, profile guest.Profile) (err error) {
	defer func() {
		if err != nil {
			err = &ProvisionError{Name: box.Name, Err: err}
		}
	}()
	run := state.ProvisionRun{PID: os.Getpid(), StartedAt: time.Now().UTC(), Step: "prepare"}
	dir := s.Store.ProvisionDir(box.Name)
	if err := s.Store.SaveProvisionRun(box.Name, run); err != nil {
		return err
	}
	var log *provisionLog
	defer func() {
		if log != nil {
			if err != nil {
				_ = log.line("Failed: "+err.Error(), false)
			}
			err = errors.Join(err, log.err, log.file.Close())
		}
		finished := time.Now().UTC()
		run.FinishedAt = &finished
		if err != nil {
			run.Error = untrusted.Line(err.Error())
		}
		err = errors.Join(err, s.Store.SaveProvisionRun(box.Name, run))
	}()
	filename := run.StartedAt.Format(provisionLogTimestamp) + ".log"
	file, err := os.OpenFile(filepath.Join(dir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	run.Log = filename
	log = &provisionLog{file: file, terminal: s.Progress}
	setStep := func(step string) error {
		run.Step = step
		return s.Store.SaveProvisionRun(box.Name, run)
	}
	if err := setStep("prepare"); err != nil {
		return err
	}
	if err := pruneProvisionLogs(dir, filename); err != nil {
		return err
	}
	worker := *s
	worker.Progress = &logFramer{emit: log.line}
	return worker.runProvision(ctx, box, profile, setStep, log)
}

func (s *Service) runProvision(ctx context.Context, box state.Box, profile guest.Profile, setStep func(string) error, log *provisionLog) error {
	restoringImage := box.Image != "" && !box.Provisioned
	instance, err := s.Driver.Get(ctx, box.Name)
	if errors.Is(err, vm.ErrNotFound) {
		if err := setStep("create"); err != nil {
			return err
		}
		var imageDisk string
		var imageFirmware []byte
		if box.Image != "" {
			image, path, err := s.Store.LoadImage(box.Image)
			if err != nil {
				return err
			}
			if err := s.checkImageCompatibility(image); err != nil {
				return err
			}
			imageDisk, imageFirmware = path, image.Firmware
		}
		s.progress("Creating %s", box.Name)
		var bootScript []byte
		var bootParameters map[string]string
		if profile.BootScript != "" {
			bootScript, err = s.Profiles.ReadScript(profile.BootScript)
			if err != nil {
				return err
			}
			bootParameters = map[string]string{
				"HOMEBOX_GATEWAY_IP": vmnet.GatewayIP,
				"HOMEBOX_SOCKET_MAC": vmnet.GuestMAC(),
			}
		}
		if err := s.Driver.Create(ctx, vm.Spec{
			Name: box.Name, ImageDisk: imageDisk, ImageFirmware: imageFirmware, Images: profile.Images, CPUs: box.CPUs, MemoryGiB: box.MemoryGiB,
			GuestOS: profile.OS, BootScript: string(bootScript), BootParameters: bootParameters,
			DiskGiB: box.DiskGiB, User: box.User, SSHPort: state.DriverSSHPort(box.Index),
			Forwards: box.Forwards,
			// Keep the built-in host resolver unused until the guest configures public DNS.
			DNS: []string{"127.0.0.1"},
		}); err != nil {
			return err
		}
		instance, err = s.Driver.Get(ctx, box.Name)
	}
	if err != nil {
		return err
	}
	if err := setStep("start"); err != nil {
		return err
	}
	// An incomplete macOS bootstrap must run again before publishing SSH.
	// Legacy Linux guests may also need a boot with the socket NIC attached.
	needsBootstrap := instance.GuestOS == vm.DarwinGuest && !box.Provisioned
	if instance.Status == vm.StatusRunning && (needsBootstrap || instance.HostPIDFile != "" && instance.Socket.Path == "") {
		s.progress("Restarting %s to configure the egress network", box.Name)
		instance, err = s.stopForProvision(ctx, box.Name)
		if err != nil {
			return err
		}
	}
	if err := s.start(ctx, &box, instance); err != nil {
		return err
	}
	if restoringImage {
		// PID 1 and journald start before cloud-init replaces the machine ID.
		if err := setStep("image-identity"); err != nil {
			return err
		}
		s.progress("Restarting %s with its new machine ID", box.Name)
		instance, err := s.stopForProvision(ctx, box.Name)
		if err != nil {
			return err
		}
		if err := s.start(ctx, &box, instance); err != nil {
			return err
		}
	}
	if err := setStep("ssh"); err != nil {
		return err
	}
	endpoint, err := s.pinSSH(ctx, &box)
	if err != nil {
		return err
	}
	if !box.ImageBuild {
		if err := s.authorizeLaptopKeys(ctx, box.Name, endpoint); err != nil {
			return err
		}
	}
	env := map[string]string{
		"HOMEBOX_USER":           box.User,
		"HOMEBOX_SOCKET_MAC":     vmnet.GuestMAC(),
		"HOMEBOX_GATEWAY_SUBNET": vmnet.Subnet,
	}
	steps := profile.Steps
	if restoringImage {
		steps = nil
		if box.BaseImage {
			steps = profile.ImageUpdate
		}
	}
	for _, step := range steps {
		if err := setStep(step.Name); err != nil {
			return err
		}
		s.progress("Provisioning %s: %s", box.Name, step.Name)
		script, err := s.Profiles.ReadScript(step.Script)
		if err != nil {
			return err
		}
		if err := log.runScript(ctx, s.GuestSSH, endpoint, script, env, step.Root); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
	}
	now := time.Now().UTC()
	box.Provisioned, box.ProvisionedAt = true, &now
	if err := s.Store.Save(box); err != nil {
		return err
	}
	s.progress("Provisioned %s", box.Name)
	return nil
}

func (s *Service) stopForProvision(ctx context.Context, name string) (vm.Instance, error) {
	if err := s.Driver.Stop(ctx, name); err != nil {
		return vm.Instance{}, err
	}
	if err := s.releaseIsolation(ctx, name); err != nil {
		return vm.Instance{}, err
	}
	return s.Driver.Get(ctx, name)
}

const (
	provisionLogTimestamp = "20060102T150405.000000000Z"
	provisionLogRetention = 5
)

type provisionLog struct {
	mu       sync.Mutex
	file     *os.File
	terminal io.Writer
	err      error
}

func (l *provisionLog) line(raw string, truncated bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Leave room for a timestamp and the truncation marker within a file line.
	const textLimit = untrusted.MaxLine - 64
	text := logText(raw, untrusted.MaxLine)
	if truncated || len(text) > textLimit {
		text = logText(raw, textLimit) + " [line truncated]"
	}
	if l.err == nil {
		_, l.err = fmt.Fprintf(l.file, "%s\t%s\n", time.Now().UTC().Format(time.RFC3339Nano), text)
	}
	if l.terminal != nil {
		_, _ = fmt.Fprintln(l.terminal, text)
	}
	return l.err
}

func (l *provisionLog) runScript(ctx context.Context, ssh SSH, endpoint vm.Endpoint, script []byte, env map[string]string, root bool) error {
	stdout, stderr := &logFramer{emit: l.line}, &logFramer{emit: l.line}
	err := ssh.RunScript(ctx, endpoint, script, env, root, stdout, stderr)
	for _, output := range []*logFramer{stdout, stderr} {
		if len(output.pending) > 0 {
			err = errors.Join(err, output.finish())
		}
	}
	return err
}

func pruneProvisionLogs(dir, current string) error {
	entries, err := os.ReadDir(dir) // sorted by filename, which is the UTC start time
	if err != nil {
		return err
	}
	var logs []string
	for _, entry := range entries {
		timestamp, ok := strings.CutSuffix(entry.Name(), ".log")
		if _, err := time.Parse(provisionLogTimestamp, timestamp); ok && err == nil && entry.Type().IsRegular() && entry.Name() != current {
			logs = append(logs, entry.Name())
		}
	}
	for _, filename := range logs[:max(0, len(logs)-(provisionLogRetention-1))] {
		if err := os.Remove(filepath.Join(dir, filename)); err != nil {
			return err
		}
	}
	return nil
}
