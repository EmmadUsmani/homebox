package autostart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/state"
)

const unit = "homebox-autostart.service"

func unitQuote(value string) string {
	escaped := strings.NewReplacer(
		"\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t", "%", "%%",
	).Replace(value)
	return "\"" + escaped + "\""
}

func (j job) unitRoot() string {
	return "Environment=" + unitQuote("HOMEBOX_HOME="+j.root) + "\n"
}

func (j job) unitFile() string {
	return "[Unit]\nDescription=Start marked homebox VMs\nWants=network-online.target\nAfter=network-online.target\n\n" +
		"[Service]\nType=oneshot\nRemainAfterExit=yes\nKillMode=process\n" +
		j.unitRoot() + "Environment=" + unitQuote("PATH="+j.path) + "\n" +
		"ExecStart=" + unitQuote(":"+j.executable) + " autostart run\n\n" +
		"[Install]\nWantedBy=default.target\n"
}

func (j job) systemd(ctx context.Context, action string, out io.Writer) error {
	path := filepath.Join(j.home, ".config", "systemd", "user", unit)
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if exists && !bytes.Contains(data, []byte("\n"+j.unitRoot())) {
		return fmt.Errorf("%s is already configured for another state directory or managed externally", path)
	}
	switch action {
	case "on":
		linger, err := command(ctx, "loginctl", "show-user", j.uid, "-p", "Linger", "--value")
		if err != nil {
			return err
		}
		if linger != "yes" {
			if _, err := command(ctx, "loginctl", "enable-linger", j.uid); err != nil {
				return fmt.Errorf("could not enable user lingering: %w\nRun sudo loginctl enable-linger %s, then rerun box autostart on", err, shellQuote(j.uid))
			}
		}
		if err := state.WritePrivateFile(path, []byte(j.unitFile())); err != nil {
			return err
		}
		if _, err := command(ctx, "systemctl", "--user", "enable", unit); err != nil {
			return err
		}
		fmt.Fprintln(out, "Autostart enabled for the next boot.")
	case "off":
		if exists {
			if _, err := command(ctx, "systemctl", "--user", "disable", unit); err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			if _, err := command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "Autostart disabled.")
	case "status":
		if !exists {
			fmt.Fprintln(out, "Autostart disabled.")
			return nil
		}
		status, statusErr := command(ctx, "systemctl", "--user", "is-enabled", unit)
		if statusErr != nil && status != "disabled" {
			return statusErr
		}
		linger, err := command(ctx, "loginctl", "show-user", j.uid, "-p", "Linger", "--value")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Autostart unit: %s. User lingering: %s.\n", status, linger)
	}
	return nil
}
