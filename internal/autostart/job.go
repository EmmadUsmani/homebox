// Package autostart installs and inspects the host's boot job.
package autostart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

type job struct {
	home, root, owner, uid, executable, path string
}

// Configure manages the boot job for the current account and state directory.
func Configure(ctx context.Context, action, root string, login bool, out io.Writer) error {
	if os.Geteuid() == 0 {
		return errors.New("configure autostart as the box owner")
	}
	owner, err := user.Current()
	if err != nil {
		return err
	}
	j := job{home: owner.HomeDir, owner: owner.Username, uid: owner.Uid, root: root}
	if action == "on" {
		j.executable, err = os.Executable()
		if err != nil {
			return err
		}
		// Jobs do not source a login shell. Preserve absolute tool directories
		// such as Homebrew's, while dropping entries relative to the current shell.
		var paths []string
		for _, path := range filepath.SplitList(os.Getenv("PATH")) {
			if filepath.IsAbs(path) {
				paths = append(paths, path)
			}
		}
		j.path = strings.Join(paths, string(os.PathListSeparator))
	}
	if login && runtime.GOOS != "darwin" {
		return errors.New("--login requires macOS")
	}
	switch runtime.GOOS {
	case "linux":
		return j.systemd(ctx, action, out)
	case "darwin":
		return j.launchd(action, login, out)
	default:
		return errors.New("autostart requires Linux systemd or macOS launchd")
	}
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	result := strings.TrimSpace(string(output))
	if err != nil {
		err = fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, result)
	}
	return result, err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
