//go:build darwin

package hostinfo

import (
	"context"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/fleetprotocol"
	"golang.org/x/sys/unix"
)

// Native utilities keep the Agent CGO-free and require no administrator access
// or additional software. Cache the bounded sample so concurrent status and
// heartbeat requests do not each start top.
var darwinResources struct {
	sync.Mutex
	sampledAt time.Time
	cpu       fleetprotocol.CPUUsage
	memory    fleetprotocol.MemoryUsage
	disks     []fleetprotocol.DiskUsage
}

func collectResourceUsage() (fleetprotocol.CPUUsage, fleetprotocol.MemoryUsage, []fleetprotocol.DiskUsage) {
	darwinResources.Lock()
	defer darwinResources.Unlock()
	if time.Since(darwinResources.sampledAt) < 5*time.Second {
		return darwinResources.cpu, darwinResources.memory, darwinResources.disks
	}
	cpu := make(chan fleetprotocol.CPUUsage, 1)
	go func() { cpu <- collectDarwinCPUUsage() }()
	darwinResources.memory = collectDarwinMemoryUsage()
	darwinResources.disks = collectDarwinDiskUsage()
	darwinResources.cpu = <-cpu
	darwinResources.sampledAt = time.Now()
	return darwinResources.cpu, darwinResources.memory, darwinResources.disks
}

func darwinCommand(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	return cmd.Output()
}

func collectDarwinCPUUsage() fleetprotocol.CPUUsage {
	unavailable := fleetprotocol.CPUUsage{ErrorCode: "FLEET_HOST_CPU_UNAVAILABLE"}
	// The first sample is cumulative; only the second represents the interval.
	raw, err := darwinCommand(4*time.Second, "/usr/bin/top", "-l", "2", "-s", "1", "-n", "0")
	if err != nil {
		return unavailable
	}
	samples := 0
	var idle float64
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "CPU usage:") {
			continue
		}
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "idle" && i > 0 {
				value, err := strconv.ParseFloat(strings.TrimSuffix(fields[i-1], "%"), 64)
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
					return unavailable
				}
				idle = value
				samples++
			}
		}
	}
	if samples != 2 {
		return unavailable
	}
	return fleetprotocol.CPUUsage{Available: true, UsagePercent: math.Round((100-idle)*100) / 100}
}

func collectDarwinMemoryUsage() fleetprotocol.MemoryUsage {
	unavailable := fleetprotocol.MemoryUsage{ErrorCode: "FLEET_HOST_MEMORY_UNAVAILABLE"}
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return unavailable
	}
	raw, err := darwinCommand(2*time.Second, "/usr/bin/vm_stat")
	if err != nil {
		return unavailable
	}
	var pageSize, availablePages uint64
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		if _, size, ok := strings.Cut(line, "page size of "); ok {
			fields := strings.Fields(size)
			if len(fields) != 0 {
				pageSize, _ = strconv.ParseUint(fields[0], 10, 64)
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || (key != "Pages free" && key != "Pages inactive" && key != "Pages speculative") {
			continue
		}
		pages, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), "."), 10, 64)
		if err != nil || seen[key] || pages > total {
			return unavailable
		}
		seen[key] = true
		availablePages += pages
	}
	// Match the Linux contract: free/reclaimable memory is not counted as used.
	// The page size comes from vm_stat (16 KiB on Apple Silicon, not always 4 KiB).
	if pageSize == 0 || len(seen) != 3 || availablePages > total/pageSize {
		return unavailable
	}
	return fleetprotocol.MemoryUsage{Available: true, TotalBytes: total, UsedBytes: total - availablePages*pageSize}
}

func collectDarwinDiskUsage() []fleetprotocol.DiskUsage {
	// Prefer the installation's data volume, including external volumes.
	mount := os.Getenv("FLEET_AGENT_DATA_DIR")
	if info, err := os.Stat(mount); err != nil || !info.IsDir() {
		mount, _ = os.Getwd()
	}
	if mount == "" || mount == "/" {
		mount = "/System/Volumes/Data"
		if _, err := os.Stat(mount); err != nil {
			mount = "/"
		}
	}
	disk := fleetprotocol.DiskUsage{Name: "data", MountPoint: mount}
	var stat unix.Statfs_t
	if err := unix.Statfs(mount, &stat); err != nil || stat.Bsize <= 0 || stat.Blocks == 0 || stat.Bavail > stat.Blocks {
		disk.ErrorCode = "FLEET_HOST_DISK_UNAVAILABLE"
	} else {
		disk.Available = true
		disk.TotalBytes = stat.Blocks * uint64(stat.Bsize)
		disk.UsedBytes = (stat.Blocks - stat.Bavail) * uint64(stat.Bsize)
		disk.MountPoint = strings.TrimRight(string(stat.Mntonname[:]), "\x00")
	}
	return []fleetprotocol.DiskUsage{disk}
}
