package hostinfo

import "github.com/EmmadUsmani/homebox/internal/execx"

// ProcessUsage reads resource use for the process named by a driver PID file.
type ProcessUsage struct{ Runner execx.Runner }

// Usage is one host process sample.

type Usage struct {
	Source        string  `json:"source"`
	CPUPercent    float64 `json:"cpu_percent"`
	ResidentBytes uint64  `json:"resident_bytes"`
	UptimeSeconds uint64  `json:"uptime_seconds"`
}
