package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/doctor"
	"github.com/EmmadUsmani/homebox/internal/state"
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
	checks = append(checks, doctor.Check{
		Name: "network-daemon", OK: s.Isolation.Running(box.Name),
		Detail: "network daemon is not running",
	})
	var hostErr error
	if !doctor.Write(out, checks) {
		hostErr = errors.New("host checks failed")
	}
	endpoint, guestErr := s.Driver.SSH(ctx, box.Name)
	if guestErr == nil {
		env := map[string]string{
			"HOMEBOX_PROXY_URL":    box.ProxyURL,
			"HOMEBOX_DNS":          box.DNS,
			"HOMEBOX_DENY_TARGETS": strings.Join(targets, " "),
			"HOMEBOX_PUBLIC_URL":   "https://example.com",
		}
		guestErr = s.GuestSSH.RunScript(ctx, endpoint, script, env, false, out, out)
	}
	if guestErr != nil {
		guestErr = fmt.Errorf("guest checks: %w", guestErr)
	}
	if err := errors.Join(hostErr, guestErr, ctx.Err()); err != nil {
		return fmt.Errorf("doctor %s: %w", box.Name, err)
	}
	return nil
}
