//go:build !linux && !windows && !darwin

package hostinfo

import "backend/internal/fleetprotocol"

func collectResourceUsage() (fleetprotocol.CPUUsage, fleetprotocol.MemoryUsage, []fleetprotocol.DiskUsage) {
	return fleetprotocol.CPUUsage{
			ErrorCode: "FLEET_HOST_CPU_UNAVAILABLE",
		}, fleetprotocol.MemoryUsage{
			ErrorCode: "FLEET_HOST_MEMORY_UNAVAILABLE",
		}, []fleetprotocol.DiskUsage{{
			Name:       "root",
			MountPoint: "/",
			ErrorCode:  "FLEET_HOST_DISK_UNAVAILABLE",
		}}
}
