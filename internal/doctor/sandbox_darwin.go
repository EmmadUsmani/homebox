package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/confine"
)

// ProbeSandbox checks both installed profiles against host files and Mach IPC.
func (Host) ProbeSandbox(ctx context.Context, profileDir string) []Check {
	checks, err := probeSandbox(ctx, profileDir)
	if err != nil {
		checks = append(checks, Check{Name: "sandbox-canary", Detail: err.Error()})
	}
	return checks
}

func probeSandbox(ctx context.Context, profileDir string) (checks []Check, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(home, ".homebox-doctor-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}()
	readPath := filepath.Join(dir, "read")
	writePath := filepath.Join(dir, "write")
	if err := os.WriteFile(readPath, []byte("homebox canary\n"), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(writePath, nil, 0600); err != nil {
		return nil, err
	}
	tempRoot, err := exec.CommandContext(ctx, "/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
	if err != nil {
		return nil, fmt.Errorf("find macOS user temporary directory: %w", err)
	}
	appTemp, err := sandboxReadCanary(strings.TrimSpace(string(tempRoot)))
	if err != nil {
		return nil, err
	}
	defer os.Remove(appTemp)
	sharedTemp, err := sandboxReadCanary("/private/tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(sharedTemp)

	operations := []struct {
		name, exe string
		args      []string
	}{
		{"home-read", "/bin/cat", []string{readPath}},
		{"home-write", "/usr/bin/tee", []string{writePath}},
		{"volumes-read", "/bin/ls", []string{"-f", "/Volumes"}},
		{"shared-read", "/bin/ls", []string{"-f", "/Users/Shared"}},
		{"app-temp-read", "/bin/cat", []string{appTemp}},
		{"shared-temp-read", "/bin/cat", []string{sharedTemp}},
	}
	profiles := []string{confine.ProfileName, confine.DaemonProfileName}
	for _, profile := range profiles {
		for _, op := range operations {
			checks = append(checks, sandboxFileDenial(ctx, filepath.Join(profileDir, profile), op.name, op.exe, op.args...))
		}
	}

	// notifyutil calls com.apple.system.notification_center. Reading the state
	// of a synthetic name distinguishes a working service from a denied lookup
	// without publishing a notification or reading user data.
	notification := fmt.Sprintf("dev.homebox.doctor.mach-canary.%d", os.Getpid())
	const notifyutil = "/usr/bin/notifyutil"
	controlCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	control := exec.CommandContext(controlCtx, notifyutil, "-g", notification)
	control.Env = append(os.Environ(), "LC_ALL=C")
	output, controlErr := control.Output()
	if controlErr != nil || strings.TrimSpace(string(output)) != notification+" 0" {
		return checks, fmt.Errorf("unsandboxed notification service control failed: %v (%q)", controlErr, output)
	}
	for _, profile := range profiles {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		output, probeErr := sandboxCommand(probeCtx, filepath.Join(profileDir, profile), notifyutil, "-g", notification).Output()
		cancel()
		check := Check{Name: sandboxCheckName(profile, "mach-lookup")}
		// NOTIFY_STATUS_FAILED (9) is printed on stdout, with exit status 0.
		check.OK = probeErr == nil && strings.TrimSpace(string(output)) == notification+": Failed with code 9"
		if !check.OK {
			check.Detail = fmt.Sprintf("expected notification service lookup to fail, got %v (%q)", probeErr, output)
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func sandboxReadCanary(dir string) (string, error) {
	file, err := os.CreateTemp(dir, ".homebox-doctor-")
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func sandboxCommand(ctx context.Context, profile, executable string, args ...string) *exec.Cmd {
	command := []string{"-f", profile, "-D", "BOX_PROBE=" + executable, executable}
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", append(command, args...)...)
	cmd.Dir = "/"
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd
}

func sandboxCheckName(profile, operation string) string {
	return strings.TrimSuffix(filepath.Base(profile), ".sb") + "-" + operation
}

func sandboxFileDenial(ctx context.Context, profile, operation, executable string, args ...string) Check {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := sandboxCommand(ctx, profile, executable, args...)
	cmd.Stdin = strings.NewReader("homebox canary\n")
	_, err := cmd.Output()
	var exitErr *exec.ExitError
	var stderr string
	if errors.As(err, &exitErr) {
		stderr = string(exitErr.Stderr)
	}
	check := Check{Name: sandboxCheckName(profile, operation)}
	want := filepath.Base(executable) + ": " + args[len(args)-1] + ": Operation not permitted"
	for _, line := range strings.Split(stderr, "\n") {
		if ctx.Err() == nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 && line == want {
			check.OK = true
			return check
		}
	}
	check.Detail = fmt.Sprintf("expected Seatbelt denial, got %v: %s", err, strings.TrimSpace(stderr))
	return check
}
