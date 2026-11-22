// Package confine prepares host-side sandbox profiles for VM processes.
package confine

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// ProfileName is the file Prepare writes in its directory.
const ProfileName = "hostagent.sb"

// DaemonProfileName is the profile used by the VM network daemon.
const DaemonProfileName = "vmnet.sb"

// Rules describes the VM process's permitted network operations.
type Rules struct {
	RuntimeDir string
	Home       string
	LimaHome   string
	Executable string
	Daemon     bool
	ConnectTCP []int
	ConnectUDP []int
	BindTCP    []Bind
	BindUDP    []Bind
}

// Bind permits a TCP listener on loopback or on the LAN.
type Bind struct {
	Port int
	LAN  bool
}

func render(rules Rules) (string, error) {
	socketPath, err := profilePath(rules.RuntimeDir)
	if err != nil {
		return "", err
	}
	var profile strings.Builder
	profile.WriteString("(version 1)\n(allow default)\n\n")
	if !rules.Daemon {
		if err := writeOutbound(&profile, rules, socketPath); err != nil {
			return "", err
		}
	}
	profile.WriteString("(deny network-bind)\n(allow network-bind\n")
	for _, local := range []struct {
		protocol string
		binds    []Bind
	}{{"tcp", rules.BindTCP}, {"udp", rules.BindUDP}} {
		for _, bind := range local.binds {
			if !validPort(bind.Port) {
				return "", fmt.Errorf("invalid %s bind port %d", local.protocol, bind.Port)
			}
			host := "localhost"
			if bind.LAN {
				host = "*"
			}
			fmt.Fprintf(&profile, "  (local %s \"%s:%d\")\n", local.protocol, host, bind.Port)
		}
	}
	if !rules.Daemon {
		fmt.Fprintf(&profile, "  (subpath %s))\n", socketPath)
	} else {
		profile.WriteString(")\n")
	}
	if err := writeFilesAndProcesses(&profile, rules, socketPath); err != nil {
		return "", err
	}
	return profile.String(), nil
}

func writeOutbound(profile *strings.Builder, rules Rules, socketPath string) error {
	profile.WriteString("(deny network-outbound)\n(allow network-outbound\n")
	for _, remote := range []struct {
		protocol string
		ports    []int
	}{{"tcp", rules.ConnectTCP}, {"udp", rules.ConnectUDP}} {
		for _, port := range remote.ports {
			if !validPort(port) {
				return fmt.Errorf("invalid %s connect port %d", remote.protocol, port)
			}
			fmt.Fprintf(profile, "  (remote %s \"localhost:%d\")\n", remote.protocol, port)
		}
	}
	fmt.Fprintf(profile, "  (subpath %s))\n\n", socketPath)
	return nil
}

func writeFilesAndProcesses(profile *strings.Builder, rules Rules, runtimeDir string) error {
	executable, err := profilePath(rules.Executable)
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	profile.WriteString(`
(deny file-write*)
(deny file-read-data file-write-data (literal "/dev/dtracehelper") (with no-log))
(deny process-exec*)
(deny appleevent-send job-creation)
(deny mach-lookup
  (global-name-regex #"^com[.]apple[.](lsd|launchservices|coreservices[.]launchservicesd|runningboard)([.]|$)"))
`)
	// The platform-signed sandbox-exec can launch the entrypoint, but the
	// daemon cannot use that exception to re-execute itself.
	profile.WriteString("(define entrypoint (param \"BOX_PROBE\"))\n")
	profile.WriteString("(allow process-exec (require-all (process-attribute is-platform-binary) (literal entrypoint)))\n")
	fmt.Fprintf(profile, "(allow file-write* (subpath %s))\n", runtimeDir)
	if rules.Daemon {
		profile.WriteString(`(deny process-fork)
(deny file-read*)
(allow file-read*
  (subpath "/System")
  (literal "/")
  (literal "/etc")
  (literal "/var")
  (subpath "/usr/lib")
  (subpath "/private/etc")
  (subpath "/private/var/db/timezone")
  (subpath "/usr/share/zoneinfo")
  (literal "/dev/null")
  (literal "/dev/urandom")
  (literal entrypoint))
`)
		fmt.Fprintf(profile, "(allow file-read* (subpath %s) (literal %s))\n", runtimeDir, executable)
		profile.WriteString("(allow file-read-metadata (literal \"/tmp\"))\n")
		for _, target := range []string{rules.RuntimeDir, rules.Executable} {
			for parent := path.Dir(target); parent != "/"; parent = path.Dir(parent) {
				quoted, _ := profilePath(parent)
				fmt.Fprintf(profile, "(allow file-read-metadata (literal %s))\n", quoted)
			}
		}
		return nil
	}
	home, err := profilePath(rules.Home)
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	limaHome, err := profilePath(rules.LimaHome)
	if err != nil {
		return fmt.Errorf("Lima home: %w", err)
	}
	fmt.Fprintf(profile, "(deny file-read* (subpath %s))\n", home)
	fmt.Fprintf(profile, "(allow file-read* (subpath %s) (literal %s))\n", limaHome, executable)
	profile.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	networks, err := profilePath(path.Join(rules.LimaHome, "_config", "networks.yaml"))
	if err != nil {
		return err
	}
	fmt.Fprintf(profile, "(allow file-write* (literal %s))\n", networks)
	fmt.Fprintf(profile, "(allow process-exec (literal %s) (literal \"/usr/bin/ssh\") (literal \"/usr/bin/sw_vers\") (literal \"/usr/sbin/ioreg\") (literal \"/usr/sbin/system_profiler\") (literal \"/bin/sh\") (literal \"/bin/bash\"))\n", executable)
	return nil
}

func profilePath(value string) (string, error) {
	if !path.IsAbs(value) || path.Clean(value) == "/" || strings.ContainsFunc(value, unicode.IsControl) {
		return "", fmt.Errorf("expected an absolute path other than the root, without control characters")
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path.Clean(value)) + `"`, nil
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}
