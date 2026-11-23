package hostinfo

import (
	"context"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/EmmadUsmani/homebox/internal/execx"
)

func (p ProcessUsage) Sample(ctx context.Context, pidFile string) *Usage {
	if pidFile == "" {
		return nil
	}
	file, err := os.Open(pidFile)
	if err != nil {
		return nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32))
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || parseErr != nil || pid <= 1 || len(data) == 32 {
		return nil
	}
	result, err := p.Runner.Run(ctx, execx.Cmd{
		Name: "/bin/ps", Args: []string{"-ww", "-p", strconv.Itoa(pid), "-o", "%cpu=,rss=,etime=,command="},
		Env: []string{"LC_ALL=C"},
	})
	if err != nil {
		return nil
	}
	// Match the full instance PID-file argument to reject a reused PID.
	command := string(result.Stdout)
	prefix, args, found := strings.Cut(command, " hostagent --pidfile ")
	fields := strings.Fields(prefix)
	if !found || len(fields) < 4 || !strings.HasPrefix(args, pidFile+" --socket ") {
		return nil
	}
	cpu, cpuErr := strconv.ParseFloat(fields[0], 64)
	rss, rssErr := strconv.ParseUint(fields[1], 10, 54)
	uptime, ok := elapsedSeconds(fields[2])
	if cpuErr != nil || math.IsNaN(cpu) || math.IsInf(cpu, 0) || cpu < 0 || rssErr != nil || !ok {
		return nil
	}
	return &Usage{Source: "hostagent", CPUPercent: cpu, ResidentBytes: rss * 1024, UptimeSeconds: uptime}
}

func elapsedSeconds(elapsed string) (uint64, bool) {
	var total uint64
	days, clock, hasDays := strings.Cut(elapsed, "-")
	if hasDays {
		value, err := strconv.ParseUint(days, 10, 32)
		if err != nil {
			return 0, false
		}
		total = value * 86400
	} else {
		clock = days
	}
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var seconds uint64
	for _, part := range parts {
		value, err := strconv.ParseUint(part, 10, 6)
		if err != nil || value >= 60 {
			return 0, false
		}
		seconds = seconds*60 + value
	}
	return total + seconds, true
}
