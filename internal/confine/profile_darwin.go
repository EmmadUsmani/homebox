package confine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Prepare installs the profile in dir and returns the command prefix for it.
// sandbox-exec loads the profile at launch.
func Prepare(rules Rules, dir string) ([]string, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("profile directory must be absolute")
	}
	installedExecutable := rules.Executable
	driverBin := filepath.Dir(installedExecutable)
	var err error
	name := ProfileName
	if rules.Daemon {
		name = DaemonProfileName
	}
	rules.RuntimeDir, err = filepath.EvalSymlinks(rules.RuntimeDir)
	if err != nil {
		return nil, err
	}
	rules.Executable, err = filepath.EvalSymlinks(rules.Executable)
	if err != nil {
		return nil, err
	}
	if !rules.Daemon {
		if installedExecutable != rules.Executable {
			rules.executableAlias = installedExecutable
		}
		rules.Home, err = filepath.EvalSymlinks(rules.Home)
		if err != nil {
			return nil, err
		}
		rules.LimaHome, err = filepath.EvalSymlinks(rules.LimaHome)
		if err != nil {
			return nil, err
		}
	}
	profile, err := render(rules)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, ".profile-*.sb")
	if err != nil {
		return nil, err
	}
	_, writeErr := file.WriteString(profile)
	closeErr := file.Close()
	path := filepath.Join(dir, name)
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(file.Name())
		return nil, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		os.Remove(file.Name())
		return nil, err
	}
	wrap := []string{"/usr/bin/sandbox-exec", "-f", path, "-D", "BOX_PROBE=" + rules.Executable}
	if rules.Daemon {
		return wrap, nil
	}
	tempDir := filepath.Join(rules.RuntimeDir, "tmp")
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return nil, err
	}
	// Lima's subprocesses need its install directory and the system tools.
	// Do not inherit user-supplied PATH entries or the host SSH agent socket.
	searchPath := strings.Join([]string{driverBin, "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
	env := []string{"/usr/bin/env", "-C", "/", "-u", "SSH_AUTH_SOCK", "PWD=/", "PATH=" + searchPath, "TMPDIR=" + tempDir}
	return append(env, wrap...), nil
}
