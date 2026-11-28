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

// QEMUSSHSocket is the private SSH forward in a Linux VM's runtime directory.
const QEMUSSHSocket = "boxssh.sock"

// Rules describes the VM process's permitted network operations.
type Rules struct {
	RuntimeDir      string
	Launcher        string
	Home            string
	LimaHome        string
	Executable      string
	executableAlias string
	Daemon          bool
	GatewaySocket   string
	HostPIDFile     string
	ConnectTCP      []int
	ConnectUDP      []int
	BindTCP         []Bind
	BindUDP         []Bind
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
	if rules.Daemon {
		profile.WriteString("(deny network-outbound)\n(allow network-outbound (remote ip4 \"*:*\"))\n\n")
	} else if err := writeOutbound(&profile, rules, socketPath); err != nil {
		return "", err
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
		if rules.GatewaySocket != "" {
			gateway, err := profilePath(rules.GatewaySocket)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&profile, "  (literal %s)\n", gateway)

		}
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
	if rules.GatewaySocket != "" {
		gateway, err := profilePath(rules.GatewaySocket)
		if err != nil {
			return err
		}
		fmt.Fprintf(profile, "  (literal %s)\n", gateway)
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
(deny file-read*)
(deny process-exec*)
(deny appleevent-send job-creation)
(deny mach-lookup)

; System libraries, framework resources and dyld shared caches.
(allow file-read*
  (subpath "/System/Library")
  (subpath "/System/Cryptexes/OS")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS")
  (subpath "/usr/lib")
  (literal "/")
  (literal "/dev/null")
  (literal "/dev/urandom")
  (literal "/etc")
  (literal "/var")
  (literal "/private/etc/localtime")
  (subpath "/private/var/db/timezone")
  (subpath "/usr/share/zoneinfo"))
(allow file-read-metadata
  (literal "/System")
  (literal "/usr")
  (literal "/usr/share")
  (literal "/private")
  (literal "/private/etc")
  (literal "/private/var")
  (literal "/private/var/db")
  (literal "/tmp"))
; Suppress the optional dyld tracing-device probe.
(deny file-read-data file-write-data (literal "/dev/dtracehelper") (with no-log))
`)
	// The platform-signed sandbox-exec can launch the entrypoint, but the
	// daemon cannot use that exception to re-execute itself.
	profile.WriteString("(define entrypoint (param \"BOX_PROBE\"))\n(allow file-read* (literal entrypoint))\n")
	profile.WriteString("(allow process-exec (require-all (process-attribute is-platform-binary) (literal entrypoint)))\n")
	fmt.Fprintf(profile, "(allow file-write* (subpath %s))\n", runtimeDir)
	if rules.Daemon {
		profile.WriteString("(deny process-fork)\n")
		fmt.Fprintf(profile, "(allow file-read* (subpath %s) (literal %s))\n", runtimeDir, executable)
		allowParents(profile, path.Dir(rules.RuntimeDir))
		allowParents(profile, path.Dir(rules.Executable))
		if rules.HostPIDFile != "" {
			pidFile, err := profilePath(rules.HostPIDFile)
			if err != nil {
				return err
			}
			fmt.Fprintf(profile, "(allow file-read* (literal %s))\n", pidFile)
			allowParents(profile, path.Dir(rules.HostPIDFile))
		}
		return nil
	}
	profile.WriteString(`
; Virtualization.framework starts the VM through its XPC service.
(allow mach-lookup (global-name "com.apple.Virtualization.VirtualMachine"))
; Newer Virtualization.framework uses dirhelper to resolve the user cache directory.
(allow mach-lookup (global-name "com.apple.bsd.dirhelper"))
; The cache-directory lookup also resolves the user's group membership.
(allow mach-lookup (global-name "com.apple.system.opendirectoryd.membership"))
; macOS account resolution supplies the local user to Lima and OpenSSH.
(allow mach-lookup (global-name "com.apple.system.opendirectoryd.libinfo"))

; Lima resolves these executables on PATH and spawns them in this profile.
(define host-tools
  (list "/usr/bin/ssh" "/usr/bin/sw_vers" "/usr/sbin/ioreg"
        "/usr/sbin/system_profiler" "/bin/sh" "/bin/bash"))
(allow process-exec (apply literal host-tools))
(allow file-read*
  (apply literal host-tools)
  (literal "/bin")
  (literal "/sbin")
  (literal "/usr/bin")
  (literal "/usr/sbin")
  (literal "/dev/tty")
  (literal "/private/etc/resolv.conf")
  (literal "/private/var/run/resolv.conf")
  (literal "/private/etc/ssh/ssh_config")
  (subpath "/private/etc/ssh/ssh_config.d")
  (subpath "/usr/share/icu")
  (subpath "/usr/share/locale"))
(allow file-read-metadata (literal "/private/var/run") (literal "/private/etc/ssh"))
`)
	allowParents(profile, path.Dir(rules.Executable))
	if rules.executableAlias != "" {
		alias, err := profilePath(rules.executableAlias)
		if err != nil {
			return err
		}
		fmt.Fprintf(profile, "(allow file-read* (literal %s))\n", alias)
		allowParents(profile, path.Dir(rules.executableAlias))
	}

	home, err := profilePath(rules.Home)
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	if _, err := profilePath(rules.LimaHome); err != nil {
		return fmt.Errorf("Lima home: %w", err)
	}
	limaConfig, _ := profilePath(path.Join(rules.LimaHome, "_config"))
	limaYAML, err := profilePath(path.Join(rules.RuntimeDir, "lima.yaml"))
	if err != nil {
		return err
	}
	// Only Lima's shared config (its SSH key) and this VM's own directory:
	// not other VMs' disks.
	fmt.Fprintf(profile, "(deny file-read* (subpath %s))\n", home)
	fmt.Fprintf(profile, "(allow file-read* (subpath %s) (subpath %s) (literal %s))\n", limaConfig, runtimeDir, executable)
	// Lima lists its home (names only), stats the gateway socket when it
	// validates the config, and resolves symlinks along both paths.
	quotedHome, _ := profilePath(rules.LimaHome)
	fmt.Fprintf(profile, "(allow file-read-data (literal %s))\n", quotedHome)
	allowParents(profile, rules.LimaHome)
	if rules.GatewaySocket != "" {
		gateway, err := profilePath(rules.GatewaySocket)
		if err != nil {
			return fmt.Errorf("gateway socket: %w", err)
		}
		fmt.Fprintf(profile, "(allow file-read-metadata (literal %s))\n", gateway)
		allowParents(profile, path.Dir(rules.GatewaySocket))
	}
	profile.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	// limactl start opens Lima's network config for writing; box never
	// configures Lima-managed networks, which are all it holds.
	networks, err := profilePath(path.Join(rules.LimaHome, "_config", "networks.yaml"))
	if err != nil {
		return err
	}
	fmt.Fprintf(profile, "(allow file-write* (literal %s))\n", networks)
	// The next start reads the VM's config outside this VM's control, so the
	// hostagent must not be able to add mounts or forwards to it.
	fmt.Fprintf(profile, "(deny file-write* (literal %s))\n", limaYAML)
	fmt.Fprintf(profile, "(allow process-exec (literal %s))\n", executable)
	return nil
}

// allowParents lets the process stat dir and every directory above it.
func allowParents(profile *strings.Builder, dir string) {
	for parent := path.Clean(dir); parent != "/"; parent = path.Dir(parent) {
		quoted, _ := profilePath(parent)
		fmt.Fprintf(profile, "(allow file-read-metadata (literal %s))\n", quoted)
	}
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
