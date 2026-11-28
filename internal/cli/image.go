package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func imageCommand(service Application, version string) *cobra.Command {
	const warning = "Warning: images include all disk data, including any credentials, agent logins and tokens."
	command := &cobra.Command{Use: "image", Short: "Manage saved VM disks"}
	command.AddCommand(
		&cobra.Command{Use: "save NAME IMAGE", Short: "Save a stopped VM as an image", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
			return service.SaveImage(cmd.Context(), args[0], args[1], version)
		}},
		&cobra.Command{Use: "ls", Short: "List local images", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			images, err := service.ListImages()
			if err != nil {
				return err
			}
			table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(table, "IMAGE\tPROFILE\tDISK\tCREATED (UTC)\tSHA-256")
			for _, image := range images {
				fmt.Fprintf(table, "%s\t%s\t%d GiB\t%s\t%s\n", image.Name, image.Profile, image.DiskGiB,
					image.CreatedAt.UTC().Format("2006-01-02 15:04:05"), image.SHA256)
			}
			return table.Flush()
		}},
		&cobra.Command{Use: "rm IMAGE", Short: "Delete a local image", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			return service.RemoveImage(args[0])
		}},
		&cobra.Command{Use: "export IMAGE FILE", Short: "Export an image to a compressed file", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
			return service.ExportImage(cmd.Context(), args[0], args[1])
		}},
	)
	var profile string
	rebuild := &cobra.Command{
		Use: "rebuild", Short: "Build a signed-out base image from a guest profile", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return service.RebuildImage(cmd.Context(), profile, version)
		},
	}
	rebuild.Flags().StringVar(&profile, "profile", "", "Guest profile (default ubuntu)")
	command.AddCommand(rebuild)
	var sourceHome string
	copyCommand := &cobra.Command{
		Use: "copy IMAGE --from-home DIR", Short: "Copy an image from a trusted local store", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if sourceHome == "" {
				return fmt.Errorf("--from-home requires a source store directory")
			}
			return service.CopyImage(cmd.Context(), args[0], sourceHome, func() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Warning: could not clone the image disk. Copying it instead, which may take several minutes.")
			})
		},
	}
	copyCommand.Flags().StringVar(&sourceHome, "from-home", "", "homebox state directory containing the source image")
	command.AddCommand(copyCommand)

	var importName string
	importCommand := &cobra.Command{
		Use: "import FILE", Short: "Verify and import an exported image", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("name") && importName == "" {
				return fmt.Errorf("--name requires a non-empty image name")
			}
			name, err := service.ImportImage(cmd.Context(), args[0], importName)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), name)
			return err
		},
	}
	importCommand.Flags().StringVar(&importName, "name", "", "save the imported disk under this image name")
	command.AddCommand(importCommand)
	return command
}
