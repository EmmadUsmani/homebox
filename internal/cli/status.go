package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/EmmadUsmani/homebox/internal/app"
	"github.com/EmmadUsmani/homebox/internal/hostinfo"
	"github.com/spf13/cobra"
)

func statusCommand(service Application) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "status NAME", Short: "Show VM health and activity", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		status, err := service.Status(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
		}
		return printStatus(cmd.OutOrStdout(), status)
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print VM status as JSON")
	return cmd
}

func printStatus(out io.Writer, status app.VMStatus) error {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	nested := "off"
	if status.NestedVirt {
		nested = "on"
	}
	fmt.Fprintf(table, "VM\t%s\nState\t%s\nUptime\t%s\nNested virtualization\t%s\nDoctor\t%s\n",
		status.Name, status.State, uptime(status), nested, doctorHealth(status))
	if result := status.Doctor; result != nil {
		fmt.Fprintf(table, "\t%s (%d passed, %d failed, %d skipped)\n", result.CheckedAt.Local().Format("2006-01-02 15:04:05 MST"),
			result.Passed, result.Failed, result.Skipped)
		for _, name := range result.FailingChecks {
			fmt.Fprintf(table, "\tFAIL %s\n", name)
		}
	}
	if run := status.Provisioning; run != nil {
		if run.FinishedAt != nil && run.Error == "" && status.State != "provision-failed" {
			fmt.Fprintf(table, "Provisioned\t%s\n", run.FinishedAt.Local().Format("2006-01-02 15:04:05 MST"))
		} else {
			if run.Step != "" {
				fmt.Fprintf(table, "Provisioning step\t%s\n", run.Step)
			}
			if run.Error != "" {
				fmt.Fprintf(table, "Provisioning error\t%s\n", run.Error)
			}
		}
	}
	if status.HostFreeBytes != nil {
		fmt.Fprintf(table, "Host state volume\t%.1f GiB available\n", float64(*status.HostFreeBytes)/(1<<30))
	}
	if status.Host != nil {
		cpu, memory := hostUsage(status.Host)
		fmt.Fprintf(table, "Host (%s)\t%s CPU, %s footprint\n", hostSource(status), cpu, memory)
		if status.HostAgent != nil {
			cpu, memory = hostUsage(status.HostAgent)
			fmt.Fprintf(table, "Hostagent\t%s CPU, %s footprint\n", cpu, memory)
		}
	}
	if status.Guest != nil {
		load, memory, disk := guestUsage(status)
		fmt.Fprintf(table, "Guest load\t%s\nGuest memory\t%s\nRoot disk\t%s\n", load, memory, disk)
		for _, process := range status.Guest.Processes {
			fmt.Fprintf(table, "Process\t%d %s (%.1f%% CPU, %.1f MiB)\n", process.PID, process.Name,
				process.CPUPercent, float64(process.ResidentBytes)/(1<<20))
		}
	}
	for _, port := range status.Ports {
		publishing := "unpublished"
		if port.Published {
			publishing = port.HostURL
		}
		owners := make([]string, 0, len(port.Processes))
		for _, process := range port.Processes {
			owners = append(owners, fmt.Sprintf("%s (pid %d)", process.Name, process.PID))
		}
		if len(owners) > 0 {
			publishing += " " + strings.Join(owners, ", ")
		}
		fmt.Fprintf(table, "TCP listener\t%s port %d, %s\n", port.Address, port.GuestPort, publishing)
	}
	for _, container := range status.Containers {
		fmt.Fprintf(table, "Container\t%s %s %s (%s)\n", container.ID, container.Name, container.Image, container.Status)
	}
	for _, agent := range status.Agents {
		fmt.Fprintf(table, "Agent\t%d %s %s\n", agent.PID, agent.Name, agent.CWD)
	}
	for _, denial := range status.Denials {
		fmt.Fprintf(table, "Network\t%s %s\n", denial.Time.Local().Format("15:04:05"),
			strings.TrimSpace(fmt.Sprintf("%s %s %s %s", denial.Message, denial.Protocol, denial.Destination, denial.Reason)))
	}
	for _, problem := range status.Errors {
		fmt.Fprintf(table, "Unavailable\t%s\n", problem)
	}
	return table.Flush()
}

func doctorHealth(status app.VMStatus) string {
	if status.Doctor == nil {
		return "never"
	}
	if status.Doctor.Failed > 0 {
		return "failed"
	}
	return "ok"
}

func uptime(status app.VMStatus) string {
	if status.UptimeSeconds == nil {
		return "-"
	}
	seconds := *status.UptimeSeconds
	return fmt.Sprintf("%dh%02dm%02ds", seconds/3600, seconds/60%60, seconds%60)
}

func hostUsage(usage *hostinfo.Usage) (cpu, memory string) {
	if usage == nil {
		return "-", "-"
	}
	memory = "-"
	if usage.MemoryBytes != nil {
		memory = fmt.Sprintf("%.1f MiB", float64(*usage.MemoryBytes)/(1<<20))
	}
	return fmt.Sprintf("%.1f%%", usage.CPUPercent), memory
}

func hostSource(status app.VMStatus) string {
	if status.Host == nil {
		return "-"
	}
	if status.Host.Source == "hostagent" {
		return "hostagent only"
	}
	return status.Host.Source
}

func guestUsage(status app.VMStatus) (load, memory, disk string) {
	load, memory, disk = "-", "-", "-"
	if guest := status.Guest; guest != nil {
		if guest.Load != nil {
			load = fmt.Sprintf("%.2f %.2f %.2f", guest.Load[0], guest.Load[1], guest.Load[2])
		}
		memory, disk = storageUsage(guest.Memory, 1<<20, "MiB"), storageUsage(guest.Disk, 1<<30, "GiB")
	}
	return load, memory, disk
}

func storageUsage(usage *app.StorageUsage, divisor float64, unit string) string {
	if usage == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f / %.1f %s", float64(usage.UsedBytes)/divisor, float64(usage.TotalBytes)/divisor, unit)
}
