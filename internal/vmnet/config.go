// Package vmnet owns the host-side network services for one VM.
package vmnet

import (
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
)

// Endpoint is the published SSH address.
type Endpoint struct {
	IP   string
	Port int
}

// Config identifies a VM and its host-side network and runtime files.
type Config struct {
	Name            string
	Socket          string
	HostPIDFile     string
	SSHPublish      Endpoint
	SSHTarget       int
	SSHTargetSocket string
	LogFile         string
	PIDFile         string
}

// Args encodes the flags following the hidden _vmnet command.
func (c Config) Args() []string {
	return []string{
		"--name=" + c.Name,
		"--socket=" + c.Socket,
		"--host-pid-file=" + c.HostPIDFile,
		"--ssh-ip=" + c.SSHPublish.IP,
		"--ssh-port=" + strconv.Itoa(c.SSHPublish.Port),
		"--ssh-target=" + strconv.Itoa(c.SSHTarget),
		"--ssh-target-socket=" + c.SSHTargetSocket,
		"--log-file=" + c.LogFile,
		"--pid-file=" + c.PIDFile,
	}
}

// ParseArgs decodes and validates the flags from Args.
func ParseArgs(args []string) (Config, error) {
	var config Config
	flags := flag.NewFlagSet("_vmnet", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&config.Name, "name", "", "VM name")
	flags.StringVar(&config.Socket, "socket", "", "gateway socket path")
	flags.StringVar(&config.HostPIDFile, "host-pid-file", "", "VM host process PID path")
	flags.StringVar(&config.SSHPublish.IP, "ssh-ip", "", "SSH listen IPv4 address")
	flags.IntVar(&config.SSHPublish.Port, "ssh-port", 0, "published SSH port")
	flags.IntVar(&config.SSHTarget, "ssh-target", 0, "driver SSH port")
	flags.StringVar(&config.SSHTargetSocket, "ssh-target-socket", "", "driver SSH Unix socket")
	flags.StringVar(&config.LogFile, "log-file", "", "log path")
	flags.StringVar(&config.PIDFile, "pid-file", "", "PID path")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected _vmnet positional arguments")
	}
	return config, config.validate()
}

func (c Config) validate() error {
	if c.Name == "" {
		return fmt.Errorf("missing VM name")
	}
	if c.SSHPublish.IP != "127.0.0.1" && c.SSHPublish.IP != "0.0.0.0" {
		return fmt.Errorf("SSH listen address must be 127.0.0.1 or 0.0.0.0")
	}
	ports := make(map[int]bool, 2)
	for _, port := range []int{c.SSHPublish.Port, c.SSHTarget} {
		if port < 1 || port > 65535 || ports[port] {
			return fmt.Errorf("invalid or duplicate VM network port %d", port)
		}
		ports[port] = true
	}
	if c.SSHTargetSocket != "" && (!filepath.IsAbs(c.SSHTargetSocket) || filepath.Clean(c.SSHTargetSocket) != c.SSHTargetSocket) {
		return fmt.Errorf("invalid driver SSH socket path")
	}
	paths := make(map[string]bool, 4)
	for _, path := range []string{c.Socket, c.HostPIDFile, c.LogFile, c.PIDFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || paths[path] {
			return fmt.Errorf("VM network paths must be distinct, clean absolute paths")
		}
		paths[path] = true
	}
	if filepath.Dir(c.Socket) != filepath.Dir(c.PIDFile) || filepath.Dir(c.Socket) != filepath.Dir(c.LogFile) {
		return fmt.Errorf("gateway socket, daemon PID and log must share a private directory")
	}
	return nil
}

func endpoint(ip string, port int) string {
	return net.JoinHostPort(ip, strconv.Itoa(port))
}
