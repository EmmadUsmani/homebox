package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/untrusted"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

// Doctor verifies host publishing and guest isolation for a named or all running boxes.
func (s *Service) Doctor(ctx context.Context, name string) error {
	if name != "" {
		box, err := s.load(name)
		if err != nil {
			return err
		}
		return s.doctorBox(ctx, box)
	}

	boxes, err := s.List(ctx)
	if err != nil {
		return err
	}
	var failures error
	running := 0
	for _, box := range boxes {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		if box.Status == vm.StatusRunning {
			running++
			failures = errors.Join(failures, s.doctorBox(ctx, box.Box))
		}
	}
	if running == 0 {
		s.progress("SKIP doctor: no running boxes")
	}
	return failures
}

func (s *Service) doctorBox(ctx context.Context, box state.Box) error {
	instance, err := s.Driver.Get(ctx, box.Name)
	if err != nil {
		return err
	}
	if instance.Status != vm.StatusRunning {
		return fmt.Errorf("box %q is %s; start it before running doctor", box.Name, instance.Status)
	}
	host, err := s.Store.LoadHost()
	if err != nil {
		return err
	}
	if s.DoctorHost == nil {
		return errors.New("host diagnostics are unavailable")
	}
	script, err := s.Profiles.ReadScript("common/doctor.sh")
	if err != nil {
		return err
	}

	s.progress("Checking %s", box.Name)
	out := s.Progress
	if out == nil {
		out = io.Discard
	}
	targets, checks, closeCanaries := s.DoctorHost.Probe(ctx, instance.HostAddress, box.SSHPort, host.LAN)
	defer closeCanaries()
	if sandbox, ok := s.DoctorHost.(interface {
		ProbeSandbox(context.Context, string) []doctor.Check
	}); ok {
		checks = append(checks, sandbox.ProbeSandbox(ctx, s.Store.BoxDir(box.Name))...)
	}
	checks = append(checks, doctor.Check{
		Name: "network-daemon", OK: s.Isolation.Running(box.Name),
		Detail: "network daemon is not running",
	})
	if relay, ok := s.DoctorHost.(interface {
		ProbeLinuxSSHRelay(context.Context, string, string, int) []doctor.Check
	}); ok {
		checks = append(checks, relay.ProbeLinuxSSHRelay(ctx, instance.SSHPIDFile, instance.RuntimeDir, state.DriverSSHPort(box.Index))...)
	} else if relay, ok := s.DoctorHost.(interface {
		ProbeSSHRelay(string, int) doctor.Check
	}); ok {
		checks = append(checks, relay.ProbeSSHRelay(instance.HostPIDFile, state.DriverSSHPort(box.Index)))
	}
	result := state.DoctorResult{}
	for _, check := range checks {
		switch {
		case check.Skipped:
			result.Skipped++
		case check.OK:
			result.Passed++
		default:
			recordDoctorFailure(&result, check.Name)
		}
	}
	var hostErr error
	if !doctor.Write(out, checks) {
		hostErr = errors.New("host checks failed")
	}
	hostFailures := result.Failed
	endpoint, guestErr := s.Driver.SSH(ctx, box.Name)
	if guestErr == nil {
		env := map[string]string{
			"HOMEBOX_BUILTIN_GATEWAY": instance.HostAddress,
			"HOMEBOX_DENY_TARGETS":    strings.Join(targets, " "),
			"HOMEBOX_PUBLIC_URL":      "https://example.com",
		}
		if runtime.GOOS == "linux" && instance.SSHPIDFile != "" {
			env["HOMEBOX_CHECK_SLIRP"] = "1"
		}
		guestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		stdout := &cappedGuestOutput{limit: 128 << 10, cancel: cancel}
		stderr := &cappedGuestOutput{limit: 16 << 10, cancel: cancel}
		guestErr = s.GuestSSH.RunScript(guestCtx, endpoint, script, env, false, stdout, stderr)
		for _, output := range []*cappedGuestOutput{stdout, stderr} {
			for _, line := range strings.Split(strings.TrimSuffix(output.buffer.String(), "\n"), "\n") {
				if line != "" {
					fmt.Fprintln(out, untrusted.Line(line))
				}
			}
		}
		countGuestDoctorChecks(&result, stdout.buffer.String())
		if stdout.exceeded || stderr.exceeded {
			guestErr = errGuestOutputLimit
		}
	}
	if guestErr != nil {
		if result.Failed == hostFailures {
			recordDoctorFailure(&result, "guest-checks")
		}
		var exit *execx.ExitError
		if errors.As(guestErr, &exit) {
			// SSH diagnostics were already printed as sanitized lines.
			guestErr = fmt.Errorf("guest checks exited with status %d", exit.ExitCode)
		} else {
			guestErr = fmt.Errorf("guest checks: %w", guestErr)
		}
	}
	result.CheckedAt = time.Now().UTC()
	saveErr := s.Store.SaveDoctorResult(box, result)
	if err := errors.Join(hostErr, guestErr, ctx.Err(), saveErr); err != nil {
		return fmt.Errorf("doctor %s: %w", box.Name, err)
	}
	return nil
}

func countGuestDoctorChecks(result *state.DoctorResult, output string) {
	for line := range strings.SplitSeq(output, "\n") {
		label, rest, _ := strings.Cut(line, " ")
		name, _, _ := strings.Cut(rest, ": ")
		if len(name) == 0 || len(name) > 256 || strings.ContainsAny(name, " \t\r") {
			continue
		}
		switch label {
		case "PASS":
			result.Passed++
		case "SKIP":
			result.Skipped++
		case "FAIL":
			recordDoctorFailure(result, name)
		}
	}
}

func recordDoctorFailure(result *state.DoctorResult, name string) {
	result.Failed++
	if len(result.FailingChecks) < 256 {
		result.FailingChecks = append(result.FailingChecks, untrusted.Line(name))
	}
}
