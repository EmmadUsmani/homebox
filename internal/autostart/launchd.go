package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/state"
)

func plistString(value string) string {
	var escaped strings.Builder
	xml.EscapeText(&escaped, []byte(value))
	return "<string>" + escaped.String() + "</string>"
}

func (j job) plistRoot() string {
	return "<key>HOMEBOX_HOME</key>" + plistString(j.root)
}

func (j job) plist(label string, login bool) string {
	userName := ""
	if !login {
		userName = "\n  <key>UserName</key>" + plistString(j.owner)
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>` + plistString(label) + userName + `
  <key>ProgramArguments</key>
  <array>` + plistString(j.executable) + `<string>autostart</string><string>run</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    ` + j.plistRoot() + `
    <key>HOME</key>` + plistString(j.home) + `
    <key>PATH</key>` + plistString(j.path) + `
  </dict>
  <key>RunAtLoad</key><true/>
  <key>AbandonProcessGroup</key><true/>
</dict>
</plist>
`
}

func (j job) launchd(action string, login bool, out io.Writer) error {
	label := "com.homebox.autostart." + j.uid
	dest := filepath.Join("/Library/LaunchDaemons", label+".plist")
	agent := filepath.Join(j.home, "Library", "LaunchAgents", label+".plist")
	if login {
		dest, agent = agent, dest
	}
	if action == "on" {
		if _, err := os.Stat(agent); err == nil {
			return fmt.Errorf("disable the existing autostart job at %s before switching modes", agent)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	data, err := os.ReadFile(dest)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if exists && !bytes.Contains(data, []byte(j.plistRoot())) {
		return fmt.Errorf("%s is already configured for another state directory or managed externally", dest)
	}
	switch action {
	case "on":
		if login {
			if err := state.WritePrivateFile(dest, []byte(j.plist(label, true))); err != nil {
				return err
			}
			fmt.Fprintln(out, "Autostart LaunchAgent installed for the next graphical login.")
			return nil
		}
		source := filepath.Join(j.root, label+".plist")
		if err := state.WritePrivateFile(source, []byte(j.plist(label, false))); err != nil {
			return err
		}
		fmt.Fprintf(out, "Install for the next boot with:\n  sudo /usr/bin/install -o root -g wheel -m 0644 %s %s\n", shellQuote(source), shellQuote(dest))
	case "off":
		if exists && login {
			if err := os.Remove(dest); err != nil {
				return err
			}
			fmt.Fprintln(out, "Autostart disabled for future logins.")
		} else if exists {
			fmt.Fprintf(out, "If loaded, unload the job, then remove it:\n  sudo /bin/launchctl bootout %s\n  sudo /bin/rm %s\n", shellQuote("system/"+label), shellQuote(dest))
		} else {
			fmt.Fprintln(out, "Autostart disabled.")
		}
	case "status":
		if exists && login {
			fmt.Fprintln(out, "Autostart LaunchAgent installed for the next graphical login.")
		} else if exists {
			fmt.Fprintln(out, "Autostart LaunchDaemon installed for the next boot.")
		} else {
			fmt.Fprintln(out, "Autostart disabled.")
		}
	}
	return nil
}
