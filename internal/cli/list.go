package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"github.com/spf13/cobra"
)

func listCommand(service Application) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "ls", Short: "List VMs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		boxes, err := service.ListStatus(cmd.Context())
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(boxes)
		}
		return printListing(cmd.OutOrStdout(), boxes)
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print VM status as JSON")
	return cmd
}

func printListing(out io.Writer, boxes []app.VMStatus) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tSTATUS\tPROFILE\tSSH\tHTTP\tHEALTH\tUPTIME\tHOST CPU\tHOST MEM\tHOST SOURCE")
	for _, box := range boxes {
		cpu, memory := hostUsage(box.Host)
		fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			box.Name, box.State, box.Profile, box.SSHPort, portRanges(box.Forwards),
			doctorHealth(box), uptime(box), cpu, memory, hostSource(box))
	}
	return table.Flush()
}

func portRanges(forwards []vm.PortForward) string {
	if len(forwards) == 0 {
		return "-"
	}
	ports := make([]int, len(forwards))
	for index, forward := range forwards {
		ports[index] = forward.HostPort
	}
	slices.Sort(ports)
	var ranges []string
	for start := 0; start < len(ports); {
		end := start
		for end+1 < len(ports) && ports[end]+1 == ports[end+1] {
			end++
		}
		label := strconv.Itoa(ports[start])
		if end > start {
			label += "-" + strconv.Itoa(ports[end])
		}
		ranges = append(ranges, label)
		start = end + 1
	}
	return strings.Join(ranges, ",")
}
