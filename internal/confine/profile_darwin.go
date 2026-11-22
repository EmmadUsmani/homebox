package confine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ProfileName is the file Prepare writes in its directory.
const ProfileName = "hostagent.sb"

// Prepare writes the Seatbelt profile to dir/ProfileName and returns the
// command prefix that runs the VM's host process under it. sandbox-exec reads
// the profile once at launch, so replacing it never affects a running VM.
func Prepare(rules Rules, dir string) ([]string, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("profile directory must be absolute")
	}
	profile, err := render(rules)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, ".hostagent-*.sb")
	if err != nil {
		return nil, err
	}
	_, writeErr := file.WriteString(profile)
	closeErr := file.Close()
	path := filepath.Join(dir, ProfileName)
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(file.Name())
		return nil, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		os.Remove(file.Name())
		return nil, err
	}
	return []string{"/usr/bin/sandbox-exec", "-f", path}, nil
}
