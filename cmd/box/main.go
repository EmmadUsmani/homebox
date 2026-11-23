// Command box creates and manages homebox development VMs.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/EmmadUsmani/homebox/internal/cli"
	"github.com/EmmadUsmani/homebox/internal/isolation"
	"github.com/EmmadUsmani/homebox/internal/vmnet"
	"github.com/EmmadUsmani/homebox/internal/wiring"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// The per-VM network daemon is box re-executing itself; it needs none of
	// the service wiring below.
	if len(os.Args) > 1 && os.Args[1] == isolation.DaemonCommand {
		os.Exit(runDaemon(os.Args[2:]))
	}
	service, err := wiring.NewService(os.Executable)
	if err != nil {
		fmt.Fprintln(os.Stderr, "box:", err)
		os.Exit(1)
	}
	os.Exit(cli.Execute(service, version))
}

func runDaemon(args []string) int {
	config, err := vmnet.ParseArgs(args)
	if err == nil {
		err = vmnet.Run(context.Background(), config)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "box "+isolation.DaemonCommand+":", err)
		return 1
	}
	return 0
}
