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
	Name       string
	ProxyPort  int
	DNSPort    int
	SSHPublish Endpoint
	SSHTarget  int
	LogFile    string
	PIDFile    string
}

// Args encodes the flags following the hidden _vmnet command.
func (config Config) Args() []string {
	return []string{
		"--name=" + config.Name,
		"--proxy-port=" + strconv.Itoa(config.ProxyPort),
		"--dns-port=" + strconv.Itoa(config.DNSPort),
		"--ssh-ip=" + config.SSHPublish.IP,
		"--ssh-port=" + strconv.Itoa(config.SSHPublish.Port),
		"--ssh-target=" + strconv.Itoa(config.SSHTarget),
		"--log-file=" + config.LogFile,
		"--pid-file=" + config.PIDFile,
	}
}

// ParseArgs decodes and validates the flags from Args.
func ParseArgs(args []string) (Config, error) {
	var config Config
	flags := flag.NewFlagSet("_vmnet", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&config.Name, "name", "", "VM name")
	flags.IntVar(&config.ProxyPort, "proxy-port", 0, "loopback proxy port")
	flags.IntVar(&config.DNSPort, "dns-port", 0, "loopback DNS port")
	flags.StringVar(&config.SSHPublish.IP, "ssh-ip", "", "SSH listen IPv4 address")
	flags.IntVar(&config.SSHPublish.Port, "ssh-port", 0, "published SSH port")
	flags.IntVar(&config.SSHTarget, "ssh-target", 0, "driver SSH port")
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

func (config Config) validate() error {
	if config.Name == "" {
		return fmt.Errorf("missing VM name")
	}
	if config.SSHPublish.IP != "127.0.0.1" && config.SSHPublish.IP != "0.0.0.0" {
		return fmt.Errorf("SSH listen address must be 127.0.0.1 or 0.0.0.0")
	}
	ports := make(map[int]bool, 4)
	for _, port := range []int{config.ProxyPort, config.DNSPort, config.SSHPublish.Port, config.SSHTarget} {
		if port < 1 || port > 65535 || ports[port] {
			return fmt.Errorf("invalid or duplicate VM network port %d", port)
		}
		ports[port] = true
	}
	if !filepath.IsAbs(config.LogFile) || !filepath.IsAbs(config.PIDFile) ||
		filepath.Clean(config.LogFile) == filepath.Clean(config.PIDFile) {
		return fmt.Errorf("log and PID files must be distinct absolute paths")
	}
	return nil
}

func endpoint(ip string, port int) string {
	return net.JoinHostPort(ip, strconv.Itoa(port))
}
