package confine

import (
	"fmt"
	"os"
	"path/filepath"
)

// Prepare writes a Seatbelt profile and returns the prefix for the VM command.
func Prepare(rules Rules, dir string) ([]string, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("profile directory must be absolute")
	}
	profile, err := render(rules)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, "hostagent-*.sb")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	_, writeErr := file.WriteString(profile)
	closeErr := file.Close()
	if writeErr != nil {
		os.Remove(path)
		return nil, writeErr
	}
	if closeErr != nil {
		os.Remove(path)
		return nil, closeErr
	}
	return []string{"/usr/bin/sandbox-exec", "-f", path}, nil
}
