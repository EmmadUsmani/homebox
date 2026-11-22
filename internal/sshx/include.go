package sshx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/state"
)

// IncludeLine tells OpenSSH where to load the local aliases.
func IncludeLine(path string) (string, error) {
	if strings.ContainsAny(path, "*?[") {
		return "", fmt.Errorf("SSH Include path %q contains glob characters", path)
	}
	quoted, err := configPath(path, false)
	if err != nil {
		return "", err
	}
	return "Include " + quoted + "\n", nil
}

var includeDirective = regexp.MustCompile(`(?i)^\s*include(?:\s+|\s*=\s*)`)

// InstallInclude prepends the generated Include while preserving the user's
// original config, including when it is a symlink to a dotfiles directory.
func InstallInclude(generatedPath string) error {
	line, err := IncludeLine(generatedPath)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".ssh", "config")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		for existing := range strings.SplitSeq(string(data), "\n") {
			if match := includeDirective.FindStringIndex(existing); match != nil {
				target := strings.TrimSpace(existing[match[1]:])
				if target == generatedPath || target == strings.TrimSpace(strings.TrimPrefix(line, "Include ")) {
					return nil
				}
			}
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		backup, err := os.CreateTemp(filepath.Dir(path), "config.homebox-backup-*")
		if err != nil {
			return err
		}
		_, writeErr := backup.Write(data)
		if err := errors.Join(writeErr, backup.Close()); err != nil {
			return errors.Join(err, os.Remove(backup.Name()))
		}
	} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot install SSH Include at %s: expected an absent file", path)
	}
	return state.WritePrivateFile(path, append([]byte(line), data...))
}
