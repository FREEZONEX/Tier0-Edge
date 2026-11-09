//go:build linux

package hostinfo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"backend/internal/fleetprotocol"
)

const cpuSamplePeriod = 200 * time.Millisecond

type cpuTimes struct {
	total uint64
	idle  uint64
}

func collectResourceUsage() (fleetprotocol.CPUUsage, fleetprotocol.MemoryUsage, []fleetprotocol.DiskUsage) {
	firstCPU, firstCPUOK := readCPUTimes("/proc/stat")
	memory := collectMemoryUsage("/proc/meminfo")
	disks := collectDiskUsage("/")

	if !firstCPUOK {
		return unavailableCPU("FLEET_HOST_CPU_UNAVAILABLE"), memory, disks
	}
	time.Sleep(cpuSamplePeriod)
	secondCPU, secondCPUOK := readCPUTimes("/proc/stat")
	if !secondCPUOK || secondCPU.total <= firstCPU.total {
		return unavailableCPU("FLEET_HOST_CPU_UNAVAILABLE"), memory, disks
	}
	totalDelta := secondCPU.total - firstCPU.total
	idleDelta := secondCPU.idle - firstCPU.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return unavailableCPU("FLEET_HOST_CPU_UNAVAILABLE"), memory, disks
	}

	usagePercent := float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
	return fleetprotocol.CPUUsage{
		UsagePercent: roundPercent(usagePercent),
		Available:    true,
	}, memory, disks
}

func unavailableCPU(code string) fleetprotocol.CPUUsage {
	return fleetprotocol.CPUUsage{ErrorCode: code}
}

func readCPUTimes(path string) (cpuTimes, bool) {
	file, err := os.Open(path)
	if err != nil {
		return cpuTimes{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuTimes{}, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}

	values := make([]uint64, 0, len(fields)-1)
	for _, raw := range fields[1:] {
		value, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			return cpuTimes{}, false
		}
		values = append(values, value)
	}
	var total uint64
	// guest and guest_nice are already included in user and nice.
	for _, value := range values[:min(len(values), 8)] {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuTimes{total: total, idle: idle}, true
}

func collectMemoryUsage(path string) fleetprotocol.MemoryUsage {
	file, err := os.Open(path)
	if err != nil {
		return fleetprotocol.MemoryUsage{ErrorCode: "FLEET_HOST_MEMORY_UNAVAILABLE"}
	}
	defer file.Close()

	var total uint64
	var available uint64
	hasAvailable := false
	var free uint64
	var buffers uint64
	var cached uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		value *= 1024
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value
		case "MemAvailable":
			available = value
			hasAvailable = true
		case "MemFree":
			free = value
		case "Buffers":
			buffers = value
		case "Cached":
			cached = value
		}
	}
	if total == 0 {
		return fleetprotocol.MemoryUsage{ErrorCode: "FLEET_HOST_MEMORY_UNAVAILABLE"}
	}
	if !hasAvailable {
		available = free + buffers + cached
	}
	if available > total {
		available = total
	}
	return fleetprotocol.MemoryUsage{
		TotalBytes: total,
		UsedBytes:  total - available,
		Available:  true,
	}
}

func collectDiskUsage(path string) []fleetprotocol.DiskUsage {
	disk := fleetprotocol.DiskUsage{
		Name:       "root",
		MountPoint: path,
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		disk.ErrorCode = "FLEET_HOST_DISK_UNAVAILABLE"
		return []fleetprotocol.DiskUsage{disk}
	}
	blockSize := uint64(stat.Bsize)
	disk.TotalBytes = stat.Blocks * blockSize
	freeBytes := stat.Bavail * blockSize
	if disk.TotalBytes == 0 || freeBytes > disk.TotalBytes {
		disk.ErrorCode = "FLEET_HOST_DISK_UNAVAILABLE"
		return []fleetprotocol.DiskUsage{disk}
	}
	disk.UsedBytes = disk.TotalBytes - freeBytes
	disk.Available = true
	return []fleetprotocol.DiskUsage{disk}
}

func roundPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return float64(int(value*100+0.5)) / 100
}
