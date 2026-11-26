// Package cli adapts Cobra commands to the box application.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/state"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Application contains the operations exposed by the CLI.
type Application interface {
	DesktopInstall(context.Context, string) error
	Desktop(context.Context, string, bool, func(string) error) error
	New(context.Context, app.NewOptions) error
	SaveImage(context.Context, string, string, string) error
	ListImages() ([]state.Image, error)
	RemoveImage(string) error
	ExportImage(context.Context, string, string) error
	ImportImage(context.Context, string) (string, error)
	Provision(context.Context, string) error
	Logs(context.Context, string, app.LogOptions, func(app.LogLine) error) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Resize(context.Context, string, vm.Size) (vm.Size, error)
	Remove(context.Context, string) error
	ListStatus(context.Context) ([]app.VMStatus, error)
	Status(context.Context, string) (app.VMStatus, error)
	Top(context.Context, string) (app.TopSnapshot, error)
	SSH(context.Context, string, []string) error
	Ports(context.Context, string, app.PortOptions) ([]vm.PortForward, error)
	SSHConfig(context.Context, app.SSHConfigOptions) (string, error)
	Repin(context.Context, string, func(old, new []string) (bool, error)) error
	Open(context.Context, string, string, bool) (string, error)
	HostInit(context.Context, app.HostInitOptions) error
	Doctor(context.Context, string) error
}

var _ Application = (*app.Service)(nil)

// Execute runs box with the process arguments and returns its exit code.
func Execute(service Application, version string) int {
	root := NewCommand(service, version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(root.ErrOrStderr(), err)
		return 1
	}
	return 0
}

// NewCommand constructs box's command tree.
func NewCommand(service Application, version string) *cobra.Command {
	return newCommand(service, version, func(input io.Reader) bool {
		file, ok := input.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	})
}

func newCommand(service Application, version string, isTerminal func(io.Reader) bool) *cobra.Command {
	root := &cobra.Command{
		Use: "box", Short: "Manage home development VMs",
		SilenceUsage: true, SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		newBoxCommand(service),
		imageCommand(service, version),
		resizeCommand(service),
		hostCommand(service),
		portsCommand(service),
		removeCommand(service, isTerminal),
		repinCommand(service, isTerminal),
		namedCommand("start NAME", "Start a VM", service.Start),
		namedCommand("stop NAME", "Stop a VM", service.Stop),
		logsCommand(service),
		namedCommand("provision NAME", "Run guest provisioning", service.Provision),
		listCommand(service),
		statusCommand(service),
		topCommand(service, isTerminal),
		&cobra.Command{Use: "ssh NAME [-- cmd...]", Short: "Open an SSH session", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return service.SSH(cmd.Context(), args[0], args[1:])
		}},
		sshConfigCommand(service),
		openCommand(service),
		desktopCommand(service),
		&cobra.Command{Use: "doctor [NAME]", Short: "Check VM isolation", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return service.Doctor(cmd.Context(), name)
		}},
		&cobra.Command{Use: "version", Short: "Print the box version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		}},
	)
	return root
}

func namedCommand(use, summary string, run func(context.Context, string) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: summary, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd.Context(), args[0])
	}}
}

func newBoxCommand(service Application) *cobra.Command {
	var options app.NewOptions
	cmd := namedCommand("new NAME", "Create and provision a VM", func(ctx context.Context, name string) error {
		options.Name = name
		return service.New(ctx, options)
	})
	cmd.Flags().StringVar(&options.Image, "image", "", "Create from a saved image")
	cmd.Flags().StringVar(&options.Profile, "profile", "", "Guest profile (default ubuntu)")
	cmd.Flags().IntVar(&options.CPUs, "cpus", 0, "Number of CPUs (defaults to the host CPU count, capped at 4)")
	cmd.Flags().IntVar(&options.MemoryGiB, "memory", 0, "Memory in GiB (defaults to half of host RAM, rounded down and limited to 2–8 GiB)")
	cmd.Flags().IntVar(&options.DiskGiB, "disk", 0, "Disk size in GiB (default 50)")
	return cmd
}

func hostCommand(service Application) *cobra.Command {
	var options app.HostInitOptions
	var lan, noLAN bool
	cmd := &cobra.Command{Use: "init", Short: "Check the VM driver and configure the host", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("lan") {
			options.LAN = &lan
		}
		if cmd.Flags().Changed("no-lan") {
			bindLAN := !noLAN
			options.LAN = &bindLAN
		}
		return service.HostInit(cmd.Context(), options)
	}}
	cmd.Flags().BoolVar(&options.SSHInclude, "ssh-include", false, "Add local VM aliases to ~/.ssh/config")
	cmd.Flags().BoolVar(&lan, "lan", false, "Publish ports on the LAN")
	cmd.Flags().BoolVar(&noLAN, "no-lan", false, "Publish ports on loopback only")
	cmd.MarkFlagsMutuallyExclusive("lan", "no-lan")
	cmd.Flags().StringArrayVar(&options.AuthorizedKeyFiles, "authorized-key", nil, "Public key file for guest SSH (repeatable)")
	host := &cobra.Command{Use: "host", Short: "Configure the VM host"}
	host.AddCommand(cmd)
	return host
}

func removeCommand(service Application, isTerminal func(io.Reader) bool) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "rm NAME", Short: "Delete a VM and its disks", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if !force {
			input := cmd.InOrStdin()
			if !isTerminal(input) {
				return errors.New("removing a VM without a terminal requires --force")
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Delete %s and its disks? [y/N] ", name)
			answer, err := bufio.NewReader(input).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "y", "yes":
			default:
				fmt.Fprintln(cmd.ErrOrStderr(), "Removal cancelled.")
				return nil
			}
		}
		return service.Remove(cmd.Context(), name)
	}}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func sshConfigCommand(service Application) *cobra.Command {
	var options app.SSHConfigOptions
	cmd := &cobra.Command{Use: "ssh-config", Short: "Print laptop SSH aliases", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		output, err := service.SSHConfig(cmd.Context(), options)
		if err != nil {
			return err
		}
		_, err = io.WriteString(cmd.OutOrStdout(), output)
		return err
	}}
	cmd.Flags().StringVar(&options.Host, "host", "", "Host address (default local mDNS name)")
	cmd.Flags().BoolVar(&options.KnownHosts, "known-hosts", false, "Print pinned laptop known_hosts entries")
	cmd.Flags().BoolVar(&options.Include, "include", false, "Print the local SSH Include line")
	cmd.Flags().BoolVar(&options.Installer, "installer", false, "Print a shell script to install laptop SSH aliases and pins")
	cmd.MarkFlagsMutuallyExclusive("host", "known-hosts", "include")
	cmd.MarkFlagsMutuallyExclusive("installer", "known-hosts", "include")
	return cmd
}

func openCommand(service Application) *cobra.Command {
	var print bool
	cmd := &cobra.Command{Use: "open NAME [GUEST_PORT]", Short: "Open a published dev server", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		port := "8000"
		if len(args) == 2 {
			port = args[1]
		}
		url, err := service.Open(cmd.Context(), args[0], port, print)
		if err != nil || url == "" {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), url)
		return err
	}}
	cmd.Flags().BoolVar(&print, "print", false, "Print the URL instead of opening it")
	return cmd
}

func portsCommand(service Application) *cobra.Command {
	var options app.PortOptions
	cmd := &cobra.Command{Use: "ports NAME", Short: "Show or change published ports", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		forwards, err := service.Ports(cmd.Context(), args[0], options)
		if err != nil {
			return err
		}
		table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "GUEST\tHOST\tBIND")
		for _, forward := range forwards {
			fmt.Fprintf(table, "%d\t%d\t%s\n", forward.GuestPort, forward.HostPort, forward.HostIP)
		}
		return table.Flush()
	}}
	cmd.Flags().StringSliceVar(&options.Set, "set", nil, "Replace all forwards with GUEST:HOST mappings (repeatable)")
	cmd.Flags().StringSliceVar(&options.Add, "add", nil, "Add GUEST:HOST mappings (repeatable)")
	cmd.Flags().StringSliceVar(&options.Remove, "rm", nil, "Remove forwards by guest port (repeatable)")
	return cmd
}
