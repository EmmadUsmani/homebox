package vm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ValidateUnixSocketPath checks the path before a host service binds it.
func ValidateUnixSocketPath(path string) error {
	var limit int
	switch runtime.GOOS {
	case "darwin":
		limit = 104
	case "linux":
		limit = 108
	default:
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := resolveSocketParent(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	for _, candidate := range []string{absolute, filepath.Join(resolved, filepath.Base(absolute))} {
		if len(candidate) >= limit {
			return fmt.Errorf("Unix socket path %q is %d bytes; the limit is %d bytes: use a shorter HOMEBOX_HOME", candidate, len(candidate), limit-1)
		}
	}
	return nil
}

func resolveSocketParent(dir string) (string, error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if !errors.Is(err, os.ErrNotExist) || filepath.Dir(dir) == dir {
		return resolved, err
	}
	parent, err := resolveSocketParent(filepath.Dir(dir))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(dir)), nil
}
