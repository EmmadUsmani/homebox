package wiring

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/confine"
)

func checkHost(ctx context.Context) error {
	if err := confine.LinuxAvailable(); err != nil {
		return err
	}
	kvm, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open KVM (check your kvm group membership): %w", err)
	}
	kvm.Close()
	for _, path := range []string{"/usr/bin/qemu-system-x86_64", "/usr/bin/qemu-img", "/usr/bin/ssh", "/usr/bin/ip"} {
		if _, err := exec.LookPath(path); err != nil {
			return fmt.Errorf("required Linux host tool: %w", err)
		}
	}
	version, err := exec.CommandContext(ctx, "limactl", "--version").Output()
	if err != nil {
		return fmt.Errorf("check Lima version: %w", err)
	}
	if strings.TrimSpace(string(version)) != "limactl version 2.3.0" {
		return fmt.Errorf("Linux host support requires Lima 2.3.0, found %s", strings.TrimSpace(string(version)))
	}
	return nil
}
