package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func repinCommand(service Application, isTerminal func(io.Reader) bool) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "repin NAME", Short: "Replace SSH pins after an intentional guest host-key change", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input, output := cmd.InOrStdin(), cmd.ErrOrStderr()
			if !yes && !isTerminal(input) {
				return errors.New("re-pinning without a terminal requires --yes")
			}
			name := args[0]
			saved := false
			err := service.Repin(cmd.Context(), name, func(old, new []string) (bool, error) {
				for _, group := range []struct {
					label string
					keys  []string
				}{{"Old", old}, {"New", new}} {
					keys := strings.Join(group.keys, "\n  ")
					if keys == "" {
						keys = "(none)"
					}
					if _, err := fmt.Fprintf(output, "%s SSH fingerprints for %s:\n  %s\n", group.label, name, keys); err != nil {
						return false, err
					}
				}
				if yes {
					saved = true
					return true, nil
				}
				if _, err := fmt.Fprint(output, "Replace the SSH pins? Confirm only if you changed the guest keys intentionally. [y/N] "); err != nil {
					return false, err
				}
				answer, err := bufio.NewReader(input).ReadString('\n')
				if err != nil {
					return false, err
				}
				answer = strings.ToLower(strings.TrimSpace(answer))
				saved = answer == "y" || answer == "yes"
				return saved, nil
			})
			if err != nil {
				return err
			}
			if !saved {
				fmt.Fprintln(output, "Re-pinning cancelled.")
				return nil
			}
			fmt.Fprintln(output, "SSH pins updated. Regenerate and re-run the laptop installer (box ssh-config --installer).")
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm that the guest host-key change was intentional")
	return cmd
}
