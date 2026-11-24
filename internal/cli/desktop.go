package cli

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

func desktopCommand(service Application) *cobra.Command {
	var install, noOpen bool
	cmd := &cobra.Command{
		Use: "desktop NAME", Short: "Connect to a VM's desktop over SSH", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			if install {
				return service.DesktopInstall(ctx, args[0])
			}
			err := service.Desktop(ctx, args[0], noOpen, func(url string) error {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), url)
				return err
			})
			if ctx.Err() != nil {
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "Install the Ubuntu desktop layer without opening a viewer")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Print the one-time URL instead of opening the browser")
	cmd.MarkFlagsMutuallyExclusive("install", "no-open")
	return cmd
}
