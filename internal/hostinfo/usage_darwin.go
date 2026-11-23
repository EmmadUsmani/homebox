package hostinfo

import (
	"context"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/execx"
	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/sys/unix"
)

type usageTarget struct {
	diskPath string
	pidFile  string
	agentPID int
}

func (p ProcessUsage) Sample(ctx context.Context, instances []vm.Instance) map[string]UsageSample {
	if len(instances) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	targets := make(map[string]usageTarget, len(instances))
	disks := make(map[string]diskIdentity, len(instances))
	diskCounts := make(map[string]int, len(instances))
	pids := make(map[int]struct{})
	for _, instance := range instances {
		target := usageTarget{pidFile: instance.HostPIDFile, agentPID: readHostPID(instance.HostPIDFile)}
		if path, disk, ok := virtualizationDisk(instance.RuntimeDir); ok {
			target.diskPath = path
			disks[path] = disk
			diskCounts[path]++
		}
		if target.agentPID > 1 {
			pids[target.agentPID] = struct{}{}
		}
		targets[instance.Name] = target
	}

	var vmPIDs map[string]int
	var starts map[int]unix.Timeval
	if len(disks) > 0 {
		starts = processStarts()
		if len(starts) > 0 {
			lsofCtx, stopLsof := context.WithTimeout(ctx, 600*time.Millisecond)
			result, err := p.Runner.Run(lsofCtx, execx.Cmd{
				Name: "/usr/sbin/lsof",
				Args: []string{"-nP", "-F0pcfDin", "-c", "com.apple.Virtualization"},
				Env:  []string{"LC_ALL=C"},
			})
			stopLsof()
			if err == nil {
				vmPIDs = virtualizationPIDs(result.Stdout, disks)
				for path, pid := range vmPIDs {
					if diskCounts[path] == 1 && starts[pid].Sec != 0 {
						pids[pid] = struct{}{}
					}
				}
			}
		}
	}
	processes := p.metrics(ctx, pids)
	samples := make(map[string]UsageSample, len(targets))
	for name, target := range targets {
		sample := UsageSample{}
		if agent, ok := processes[target.agentPID]; ok && hostagentCommand(agent.command, target.pidFile) {
			agent.usage.Source = "hostagent"
			sample.HostAgent = &agent.usage
		}
		vmPID := vmPIDs[target.diskPath]
		if machine, ok := processes[vmPID]; ok && diskCounts[target.diskPath] == 1 && starts[vmPID].Sec != 0 {
			sample.VM = machine.virtualizationUsage(starts[vmPID], processStart(vmPID))
		}
		samples[name] = sample
	}
	return samples
}

func readHostPID(path string) int {
	if path == "" {
		return 0
	}
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32))
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || parseErr != nil || pid <= 1 || len(data) == 32 {
		return 0
	}
	return pid
}

func processStarts() map[int]unix.Timeval {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Geteuid())
	if err != nil {
		return nil
	}
	starts := make(map[int]unix.Timeval, len(processes))
	for _, process := range processes {
		starts[int(process.Proc.P_pid)] = process.Proc.P_starttime
	}
	return starts
}

func processStart(pid int) unix.Timeval {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return unix.Timeval{}
	}
	return process.Proc.P_starttime
}

func sameProcess(before, after unix.Timeval) bool {
	return before.Sec != 0 && before.Sec == after.Sec && before.Usec == after.Usec
}

type processMetrics struct {
	usage   Usage
	command string
}

func (metrics processMetrics) virtualizationUsage(before, after unix.Timeval) *Usage {
	if !sameProcess(before, after) || !virtualizationCommand(metrics.command) {
		return nil
	}
	metrics.usage.Source = "virtualization"
	return &metrics.usage
}

func (p ProcessUsage) metrics(ctx context.Context, pids map[int]struct{}) map[int]processMetrics {
	if len(pids) == 0 {
		return nil
	}
	ids := make([]string, 0, len(pids))
	for pid := range pids {
		ids = append(ids, strconv.Itoa(pid))
	}
	slices.Sort(ids)
	result, err := p.Runner.Run(ctx, execx.Cmd{
		Name: "/bin/ps", Args: []string{"-ww", "-p", strings.Join(ids, ","), "-o", "pid=,%cpu=,rss=,etime=,command="},
		Env: []string{"LC_ALL=C"},
	})
	if err != nil {
		return nil
	}
	processes := make(map[int]processMetrics, len(pids))
	for line := range strings.SplitSeq(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		cpu, cpuErr := strconv.ParseFloat(fields[1], 64)
		rss, rssErr := strconv.ParseUint(fields[2], 10, 54)
		uptime, ok := elapsedSeconds(fields[3])
		if pidErr != nil || cpuErr != nil || math.IsNaN(cpu) || math.IsInf(cpu, 0) || cpu < 0 || rssErr != nil || !ok {
			continue
		}
		command := strings.TrimLeft(line, " 	")
		for _, field := range fields[:4] {
			command = strings.TrimLeft(command[len(field):], " 	")
		}
		processes[pid] = processMetrics{Usage{PID: pid, CPUPercent: cpu, ResidentBytes: rss * 1024, UptimeSeconds: uptime}, command}
	}
	return processes
}

func hostagentCommand(command, pidFile string) bool {
	_, args, found := strings.Cut(command, " hostagent --pidfile ")
	return found && strings.HasPrefix(args, pidFile+" --socket ")
}

func virtualizationCommand(command string) bool {
	return strings.HasPrefix(command, "/System/Library/Frameworks/Virtualization.framework/") &&
		strings.HasSuffix(command, "/"+virtualizationProcess)
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
