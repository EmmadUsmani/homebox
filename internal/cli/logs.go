package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/spf13/cobra"
)

func logsCommand(service Application) *cobra.Command {
	var options app.LogOptions
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "logs NAME", Short: "Print or follow VM logs", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			out := cmd.OutOrStdout()
			encoder := json.NewEncoder(out)
			err := service.Logs(ctx, args[0], options, func(line app.LogLine) error {
				if jsonOutput {
					return encoder.Encode(line)
				}
				text := line.Text
				if line.Unit != "" {
					text = line.Unit + ": " + text
				}
				if line.Level != "" {
					text = line.Level + " " + text
				}
				_, err := fmt.Fprintf(out, "%s %s %s\n", line.Time.Local().Format("15:04:05"), line.Source, text)
				return err
			})
			if options.Follow && ctx.Err() == context.Canceled {
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&options.Follow, "follow", "f", false, "Follow new log lines")
	cmd.Flags().StringVar(&options.Source, "source", "", "Show one source: provision, host, net, or guest")
	cmd.Flags().StringVar(&options.Unit, "unit", "", "Filter by a guest systemd system unit")
	cmd.Flags().IntVarP(&options.Lines, "lines", "n", 100, "Number of recent lines per source (0–1000)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print one JSON object per line")
	return cmd
}
