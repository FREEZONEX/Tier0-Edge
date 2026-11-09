package hostinfo

import (
	"os"
	"runtime"

	"backend/internal/fleetprotocol"
)

type Snapshot = fleetprotocol.HostSnapshot

func Collect() Snapshot {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	cpu, memory, disks := collectResourceUsage()

	return Snapshot{
		Hostname:   hostname,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		LogicalCPU: runtime.NumCPU(),
		CPU:        cpu,
		Memory:     memory,
		Disks:      disks,
	}
}
