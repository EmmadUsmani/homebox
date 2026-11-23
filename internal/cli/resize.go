package cli

import (
	"fmt"

	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/spf13/cobra"
)

func resizeCommand(service Application) *cobra.Command {
	var changes vm.Size
	cmd := &cobra.Command{
		Use:   "resize NAME",
		Short: "Show or change a VM's size",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, flag := range []struct {
				name  string
				value int
			}{{"cpus", changes.CPUs}, {"memory", changes.MemoryGiB}, {"disk", changes.DiskGiB}} {
				if cmd.Flags().Changed(flag.name) && flag.value <= 0 {
					return fmt.Errorf("--%s must be positive", flag.name)
				}
			}
			size, err := service.Resize(cmd.Context(), args[0], changes)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %d CPUs, %d GiB memory, %d GiB disk\n", args[0], size.CPUs, size.MemoryGiB, size.DiskGiB)
			return err
		},
	}
	cmd.Flags().IntVar(&changes.CPUs, "cpus", 0, "Number of CPUs")
	cmd.Flags().IntVar(&changes.MemoryGiB, "memory", 0, "Memory in GiB")
	cmd.Flags().IntVar(&changes.DiskGiB, "disk", 0, "Disk size in GiB (grow only)")
	return cmd
}
