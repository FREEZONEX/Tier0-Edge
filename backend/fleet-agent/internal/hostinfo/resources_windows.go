//go:build windows

package hostinfo

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"backend/internal/fleetprotocol"
	"golang.org/x/sys/windows"
)

const cpuSamplePeriod = 200 * time.Millisecond

type cpuTimes struct {
	total uint64
	idle  uint64
}

var (
	kernel32DLL              = windows.NewLazySystemDLL("kernel32.dll")
	getSystemTimesProc       = kernel32DLL.NewProc("GetSystemTimes")
	globalMemoryStatusExProc = kernel32DLL.NewProc("GlobalMemoryStatusEx")
)

type windowsMemoryStatus struct {
	length            uint32
	memoryLoad        uint32
	totalPhysical     uint64
	availablePhysical uint64
	totalPageFile     uint64
	availablePageFile uint64
	totalVirtual      uint64
	availableVirtual  uint64
	availableExtended uint64
}

func collectResourceUsage() (fleetprotocol.CPUUsage, fleetprotocol.MemoryUsage, []fleetprotocol.DiskUsage) {
	firstCPU, firstCPUOK := readWindowsCPUTimes()
	memory := collectWindowsMemoryUsage()
	disks := collectWindowsDiskUsage()

	if !firstCPUOK {
		return unavailableWindowsCPU(), memory, disks
	}
	time.Sleep(cpuSamplePeriod)
	secondCPU, secondCPUOK := readWindowsCPUTimes()
	if !secondCPUOK || secondCPU.total <= firstCPU.total {
		return unavailableWindowsCPU(), memory, disks
	}

	totalDelta := secondCPU.total - firstCPU.total
	idleDelta := secondCPU.idle - firstCPU.idle
	if idleDelta > totalDelta {
		return unavailableWindowsCPU(), memory, disks
	}

	return fleetprotocol.CPUUsage{
		UsagePercent: roundPercent(float64(totalDelta-idleDelta) * 100 / float64(totalDelta)),
		Available:    true,
	}, memory, disks
}

func readWindowsCPUTimes() (cpuTimes, bool) {
	var idle syscall.Filetime
	var kernel syscall.Filetime
	var user syscall.Filetime
	result, _, _ := getSystemTimesProc.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return cpuTimes{}, false
	}

	idleValue := filetimeValue(idle)
	return cpuTimes{
		total: filetimeValue(kernel) + filetimeValue(user),
		idle:  idleValue,
	}, true
}

func filetimeValue(value syscall.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func unavailableWindowsCPU() fleetprotocol.CPUUsage {
	return fleetprotocol.CPUUsage{ErrorCode: "FLEET_HOST_CPU_UNAVAILABLE"}
}

func collectWindowsMemoryUsage() fleetprotocol.MemoryUsage {
	status := windowsMemoryStatus{length: uint32(unsafe.Sizeof(windowsMemoryStatus{}))}
	result, _, _ := globalMemoryStatusExProc.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 || status.totalPhysical == 0 || status.availablePhysical > status.totalPhysical {
		return fleetprotocol.MemoryUsage{ErrorCode: "FLEET_HOST_MEMORY_UNAVAILABLE"}
	}

	return fleetprotocol.MemoryUsage{
		TotalBytes: status.totalPhysical,
		UsedBytes:  status.totalPhysical - status.availablePhysical,
		Available:  true,
	}
}

func collectWindowsDiskUsage() []fleetprotocol.DiskUsage {
	root := windowsSystemRoot()
	disk := fleetprotocol.DiskUsage{Name: root, MountPoint: root}
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		disk.ErrorCode = "FLEET_HOST_DISK_UNAVAILABLE"
		return []fleetprotocol.DiskUsage{disk}
	}

	var available uint64
	var total uint64
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(rootPointer, &available, &total, &free); err != nil || total == 0 || free > total {
		disk.ErrorCode = "FLEET_HOST_DISK_UNAVAILABLE"
		return []fleetprotocol.DiskUsage{disk}
	}

	disk.TotalBytes = total
	disk.UsedBytes = total - free
	disk.Available = true
	return []fleetprotocol.DiskUsage{disk}
}

func windowsSystemRoot() string {
	if workingDirectory, err := os.Getwd(); err == nil {
		if volume := filepath.VolumeName(workingDirectory); volume != "" {
			return volume + `\`
		}
	}
	if volume := os.Getenv("SystemDrive"); volume != "" {
		return volume + `\`
	}
	return `C:\`
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
