package hostinfo

import "github.com/EmmadUsmani/homebox/internal/execx"

// ProcessUsage samples host processes associated with driver instances.
type ProcessUsage struct{ Runner execx.Runner }

// Usage is one host process sample.
type Usage struct {
	Source        string  `json:"source"`
	PID           int     `json:"pid"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryBytes   *uint64 `json:"memory_bytes,omitempty"` // platform-specific resident memory measure
	UptimeSeconds uint64  `json:"uptime_seconds"`
}

// UsageSample keeps the VM process and driver process readings separate.
type UsageSample struct {
	VM        *Usage
	HostAgent *Usage
}
