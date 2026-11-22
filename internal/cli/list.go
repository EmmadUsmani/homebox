package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/vm"
)

func printListing(out io.Writer, boxes []app.Listing) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tSTATUS\tPROFILE\tSSH\tHTTP")
	for _, box := range boxes {
		fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%s\n",
			box.Name, box.Status, box.Profile, box.SSHPort, portRanges(box.Forwards))
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
