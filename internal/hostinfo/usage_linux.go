package hostinfo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/vm"
	"golang.org/x/sys/unix"
)

func (ProcessUsage) Sample(ctx context.Context, instances []vm.Instance) map[string]UsageSample {
	var uptime unix.Timespec
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &uptime) != nil {
		return nil
	}
	samples := make(map[string]UsageSample, len(instances))
	for _, inst := range instances {
		if ctx.Err() != nil {
			break
		}
		samples[inst.Name] = UsageSample{
			VM:        linuxProcessUsage(inst.SSHPIDFile, "qemu", uptime),
			HostAgent: linuxProcessUsage(inst.HostPIDFile, "hostagent", uptime),
		}
	}
	return samples
}

func linuxProcessUsage(pidFile, source string, uptime unix.Timespec) *Usage {
	fd, err := unix.Open(pidFile, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil
	}
	f := os.NewFile(uintptr(fd), pidFile)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32 {
		return nil
	}
	pidData, err := io.ReadAll(io.LimitReader(f, 33))
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || parseErr != nil || pid <= 1 {
		return nil
	}
	base := fmt.Sprintf("/proc/%d/", pid)
	stats, err := os.ReadFile(base + "stat")
	if err != nil || len(stats) == 0 {
		return nil
	}
	fields := strings.Fields(string(stats[bytes.LastIndexByte(stats, ')')+1:]))
	if len(fields) < 22 || fields[0] == "Z" || fields[0] == "X" {
		return nil
	}
	utime, e1 := strconv.ParseUint(fields[11], 10, 64)
	stime, e2 := strconv.ParseUint(fields[12], 10, 64)
	start, e3 := strconv.ParseUint(fields[19], 10, 64)
	pages, e4 := strconv.ParseUint(fields[21], 10, 64)
	const userHZ = 100 // Linux x86-64 procfs ticks per second.
	age := float64(uptime.Nano())/float64(time.Second) - float64(start)/userHZ
	started := time.Now().Add(-time.Duration(age * float64(time.Second)))
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || age <= 0 || started.After(info.ModTime().Add(10*time.Millisecond)) {
		return nil
	}
	args, err := os.ReadFile(base + "cmdline")
	arguments := bytes.Split(args, []byte{0})
	index := slices.IndexFunc(arguments, func(argument []byte) bool {
		return bytes.Equal(argument, []byte("-pidfile")) || bytes.Equal(argument, []byte("--pidfile"))
	})
	if err != nil || index < 0 || index+1 >= len(arguments) || string(arguments[index+1]) != pidFile {
		return nil
	}
	current, err := os.ReadFile(base + "stat")
	currentFields := strings.Fields(string(current[bytes.LastIndexByte(current, ')')+1:]))
	if err != nil || len(currentFields) < 22 || currentFields[19] != fields[19] || currentFields[0] == "Z" || currentFields[0] == "X" {
		return nil
	}
	rss := pages * uint64(os.Getpagesize())
	return &Usage{Source: source, PID: pid, CPUPercent: float64(utime+stime) / age,
		UptimeSeconds: uint64(age), MemoryBytes: &rss}
}
