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

// ProbeSandbox checks that both installed profiles deny reading and writing
// a private fixture in the host user's home directory.
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
	operations := []struct {
		name, exe, path string
	}{
		{"read", "/bin/cat", readPath},
		{"write", "/usr/bin/tee", writePath},
	}
	for _, profile := range []string{confine.ProfileName, confine.DaemonProfileName} {
		for _, op := range operations {
			checks = append(checks, sandboxDenial(ctx, filepath.Join(profileDir, profile), op.name, op.exe, op.path))
		}
	}
	return checks, nil
}

func sandboxDenial(ctx context.Context, profile, operation, executable, target string) Check {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-f", profile, "-D", "BOX_PROBE="+executable, executable, target)
	cmd.Dir = "/"
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdin = strings.NewReader("homebox canary\n")
	_, err := cmd.Output()
	var exitErr *exec.ExitError
	var stderr string
	if errors.As(err, &exitErr) {
		stderr = string(exitErr.Stderr)
	}
	check := Check{Name: strings.TrimSuffix(filepath.Base(profile), ".sb") + "-home-" + operation}
	want := filepath.Base(executable) + ": " + target + ": Operation not permitted"
	for _, line := range strings.Split(stderr, "\n") {
		if ctx.Err() == nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 && line == want {
			check.OK = true
			return check
		}
	}
	check.Detail = fmt.Sprintf("expected Seatbelt denial, got %v: %s", err, strings.TrimSpace(stderr))
	return check
}
