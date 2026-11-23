package app

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/EmmadUsmani/homebox/internal/untrusted"
)

const guestProbeTimeout = 9 * time.Second

var errGuestOutputLimit = errors.New("guest output exceeded the byte limit")

type cappedGuestOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (output *cappedGuestOutput) Write(data []byte) (int, error) {
	remaining := output.limit - output.buffer.Len()
	if len(data) > remaining {
		n, _ := output.buffer.Write(data[:remaining])
		output.exceeded = true
		output.cancel()
		return n, errGuestOutputLimit
	}
	return output.buffer.Write(data)
}

func (s *Service) probeGuest(ctx context.Context, status *VMStatus, detail bool) {
	ctx, cancel := context.WithTimeout(ctx, guestProbeTimeout)
	defer cancel()
	script, err := s.Profiles.ReadScript("common/status.sh")
	if err != nil {
		status.addError("guest probe script", err)
		return
	}
	endpoint, err := s.Driver.SSH(ctx, status.Name)
	if err != nil {
		status.addError("guest SSH endpoint", err)
		return
	}
	var env map[string]string
	if detail {
		env = map[string]string{"HOMEBOX_STATUS_MODE": "detail"}
	}
	stdout := &cappedGuestOutput{limit: 248 << 10, cancel: cancel}
	stderr := &cappedGuestOutput{limit: 8 << 10, cancel: cancel}
	err = s.GuestSSH.RunScript(ctx, endpoint, script, env, false, stdout, stderr)
	switch {
	case stdout.exceeded || stderr.exceeded:
		status.addError("guest probe", errGuestOutputLimit)
	case ctx.Err() != nil:
		status.addError("guest probe", ctx.Err())
	case err != nil:
		status.addError("guest probe", err)
	}
	complete := ""
	if end := bytes.LastIndexByte(stdout.buffer.Bytes(), '\n'); end >= 0 {
		complete = string(stdout.buffer.Bytes()[:end+1])
	}
	parseGuestStatus(complete, status, detail)
	matchPorts(status.Ports, status.Forwards)
	if status.Guest == nil && len(status.Errors) == 0 {
		status.addError("guest probe", errors.New("no usable resource readings"))
	}
}

func parseGuestStatus(output string, status *VMStatus, detail bool) {
	var guest GuestStatus
	for line := range strings.SplitSeq(output, "\n") {
		if len(line) > untrusted.MaxLine {
			continue
		}
		kind, rest, _ := strings.Cut(line, " ")
		switch kind {
		case "uptime":
			if value, ok := probeNumber(rest); ok && value < float64(uint64(1)<<63) {
				seconds := uint64(value)
				guest.UptimeSeconds = &seconds
			}
		case "load":
			guest.Load = parseLoad(rest)
		case "memory":
			guest.Memory = parseStorage(rest, true)
		case "disk":
			guest.Disk = parseStorage(rest, false)
		default:
			if detail {
				parseGuestDetail(kind, rest, status, &guest)
			}
		}
	}
	if guest.UptimeSeconds != nil || guest.Load != nil || guest.Memory != nil || guest.Disk != nil || len(guest.Processes) > 0 {
		status.Guest = &guest
		if guest.UptimeSeconds != nil {
			status.UptimeSeconds = guest.UptimeSeconds
		}
	}
}

func probeNumber(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(raw, 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func parseLoad(raw string) *[3]float64 {
	fields := strings.Fields(raw)
	if len(fields) != 3 {
		return nil
	}
	var load [3]float64
	for index, field := range fields {
		value, ok := probeNumber(field)
		if !ok {
			return nil
		}
		load[index] = value
	}
	return &load
}

func parseStorage(raw string, available bool) *StorageUsage {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return nil
	}
	total, totalErr := strconv.ParseUint(fields[0], 10, 54)
	amount, amountErr := strconv.ParseUint(fields[1], 10, 54)
	if totalErr != nil || amountErr != nil || total == 0 || amount > total {
		return nil
	}
	if available {
		amount = total - amount
	}
	return &StorageUsage{TotalBytes: total * 1024, UsedBytes: amount * 1024}
}

func parseGuestDetail(kind, raw string, status *VMStatus, guest *GuestStatus) {
	parts := strings.SplitN(raw, " ", 4)
	switch kind {
	case "process":
		if len(parts) != 4 || len(guest.Processes) >= 10 {
			return
		}
		pid, pidErr := strconv.ParseUint(parts[0], 10, 31)
		cpu, cpuOK := probeNumber(parts[1])
		rss, rssErr := strconv.ParseUint(parts[2], 10, 54)
		name := untrusted.Line(parts[3])
		if pidErr == nil && pid > 0 && cpuOK && rssErr == nil && name != "" {
			guest.Processes = append(guest.Processes, ProcessStatus{int(pid), name, cpu, rss * 1024})
		}
	case "listener":
		address, process, _ := strings.Cut(raw, " ")
		host, portText, err := net.SplitHostPort(address)
		port, portErr := strconv.ParseUint(portText, 10, 16)
		_, addrErr := netip.ParseAddr(host)
		if err == nil && portErr == nil && port > 0 && len(host) <= 128 &&
			(host == "*" || addrErr == nil) && len(status.Ports) < 200 {
			status.Ports = append(status.Ports, PortStatus{Address: untrusted.Line(host), GuestPort: int(port), Process: untrusted.Line(process)})
		}
	case "container":
		if len(parts) == 4 && len(status.Containers) < 100 {
			status.Containers = append(status.Containers, ContainerStatus{
				untrusted.Line(parts[0]), untrusted.Line(parts[1]), untrusted.Line(parts[2]), untrusted.Line(parts[3]),
			})
		}
	case "agent":
		fields := strings.SplitN(raw, " ", 3)
		if len(fields) != 3 || len(status.Agents) >= 100 || (fields[1] != "claude" && fields[1] != "codex") {
			return
		}
		if pid, err := strconv.ParseUint(fields[0], 10, 31); err == nil && pid > 0 {
			status.Agents = append(status.Agents, AgentStatus{int(pid), untrusted.Line(fields[1]), untrusted.Line(fields[2])})
		}
	}
}
