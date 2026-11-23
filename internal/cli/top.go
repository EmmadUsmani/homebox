package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/spf13/cobra"
)

func topCommand(service Application, isTerminal func(io.Reader) bool) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "top [NAME]", Short: "Watch running VMs' resource use", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()
		out := cmd.OutOrStdout()
		terminal, ok := out.(io.Reader)
		jsonLines := asJSON || !ok || !isTerminal(terminal)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		encoder := json.NewEncoder(out)
		for ctx.Err() == nil {
			snapshot, err := service.Top(ctx, name)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			if jsonLines {
				err = encoder.Encode(snapshot)
			} else {
				err = printTop(out, snapshot)
			}
			if err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print one JSON object per refresh")
	return cmd
}

func printTop(out io.Writer, snapshot app.TopSnapshot) error {
	if _, err := fmt.Fprint(out, "\x1b[H\x1b[2J"); err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tHOST CPU\tHOST MEM\tHOST SOURCE\tGUEST LOAD\tGUEST MEM\tROOT DISK\tERRORS")
	for _, status := range snapshot.VMs {
		cpu, hostMemory := hostUsage(status.Host)
		load, guestMemory, disk := guestUsage(status)
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", status.Name, cpu, hostMemory, hostSource(status), load, guestMemory, disk, strings.Join(status.Errors, ", "))
	}
	return table.Flush()
}
