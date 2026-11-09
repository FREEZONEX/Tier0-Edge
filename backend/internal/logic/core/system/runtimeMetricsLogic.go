// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	respx "backend/internal/httpx"
	"backend/internal/runtimeenv"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	runtimeDiskPath        = "/app/data/files"
	runtimeCpuSamplePeriod = time.Second
	hostProcPath           = "/host/proc"
)

type RuntimeMetricsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

type cpuSnapshot struct {
	total uint64
	idle  uint64
	cores int
}

func NewRuntimeMetricsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RuntimeMetricsLogic {
	return &RuntimeMetricsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RuntimeMetricsLogic) RuntimeMetrics() (resp *types.Envelope, err error) {
	if runtimeenv.IsNative() {
		host, err := l.svcCtx.App.Cluster.CurrentHost(l.ctx)
		if err != nil {
			return nil, err
		}
		metrics := map[string]any{
			"cpu": map[string]any{
				"usagePercent": host.CPU.UsagePercent, "logicalCores": host.LogicalCPU,
				"available": host.CPU.Available, "scope": "host", "source": "fleet-agent",
			},
			"memory": map[string]any{
				"usagePercent": percent(host.Memory.UsedBytes, host.Memory.TotalBytes),
				"usedBytes":    host.Memory.UsedBytes, "totalBytes": host.Memory.TotalBytes,
				"available": host.Memory.Available, "scope": "host", "source": "fleet-agent",
			},
			"collectedAt": time.Now().UnixMilli(),
		}
		if len(host.Disks) > 0 {
			disk := host.Disks[0]
			metrics["disk"] = map[string]any{
				"usagePercent": percent(disk.UsedBytes, disk.TotalBytes),
				"usedBytes":    disk.UsedBytes, "totalBytes": disk.TotalBytes,
				"available": disk.Available, "path": disk.MountPoint,
			}
		}
		return respx.Envelope(metrics), nil
	}
	cpuPercent := sampleCPUPercent("/proc/stat", runtimeCpuSamplePeriod)
	memUsed, memTotal := memoryUsage("/proc/meminfo")
	diskUsed, diskTotal := diskUsage(runtimeDiskPath)

	metrics := map[string]any{
		"cpu": map[string]any{
			"usagePercent": roundPercent(cpuPercent),
			"logicalCores": countCPUCores("/proc/stat"),
			"scope":        "runtime",
			"source":       "/proc/stat",
		},
		"memory": map[string]any{
			"usagePercent": percent(memUsed, memTotal),
			"usedBytes":    memUsed,
			"totalBytes":   memTotal,
			"scope":        "runtime",
			"source":       "/proc/meminfo",
		},
		"disk": map[string]any{
			"usagePercent": percent(diskUsed, diskTotal),
			"usedBytes":    diskUsed,
			"totalBytes":   diskTotal,
			"path":         runtimeDiskPath,
		},
		"collectedAt": time.Now().UnixMilli(),
	}
	if hostMetricsEnabled() {
		hostStat := hostProcPath + "/stat"
		hostMemInfo := hostProcPath + "/meminfo"
		// 挂载的宿主机 /proc 可能是空目录（如 macOS/Windows 下的占位目录），
		// 只有能读到有效数据时才返回 host 指标，否则让前端回落到 runtime 指标。
		if _, ok := readCPUSnapshot(hostStat); ok {
			hostCPUPercent := sampleCPUPercent(hostStat, runtimeCpuSamplePeriod)
			metrics["hostCpu"] = map[string]any{
				"usagePercent": roundPercent(hostCPUPercent),
				"logicalCores": countCPUCores(hostStat),
				"scope":        "host",
				"source":       hostStat,
			}
		} else {
			logx.Infof("[runtimeMetrics] host cpu stats unavailable at %s, omitting hostCpu", hostStat)
		}
		if hostMemUsed, hostMemTotal := memoryUsage(hostMemInfo); hostMemTotal > 0 {
			metrics["hostMemory"] = map[string]any{
				"usagePercent": percent(hostMemUsed, hostMemTotal),
				"usedBytes":    hostMemUsed,
				"totalBytes":   hostMemTotal,
				"scope":        "host",
				"source":       hostMemInfo,
			}
		} else {
			logx.Infof("[runtimeMetrics] host memory stats unavailable at %s, omitting hostMemory", hostMemInfo)
		}
	}

	return respx.Envelope(metrics), nil
}

func hostMetricsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HOST_METRICS_ENABLED"))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func sampleCPUPercent(statPath string, interval time.Duration) float64 {
	first, ok := readCPUSnapshot(statPath)
	if !ok {
		return 0
	}
	time.Sleep(interval)
	second, ok := readCPUSnapshot(statPath)
	if !ok || second.total <= first.total {
		return 0
	}
	totalDelta := second.total - first.total
	idleDelta := second.idle - first.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta)
}

func readCPUSnapshot(statPath string) (cpuSnapshot, bool) {
	file, err := os.Open(statPath)
	if err != nil {
		return cpuSnapshot{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuSnapshot{}, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSnapshot{}, false
	}
	var values []uint64
	for _, raw := range fields[1:] {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cpuSnapshot{}, false
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	cores := 0
	for scanner.Scan() {
		name := strings.Fields(scanner.Text())
		if len(name) == 0 || !strings.HasPrefix(name[0], "cpu") {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(name[0], "cpu")); err == nil {
			cores++
		}
	}
	return cpuSnapshot{total: total, idle: idle, cores: cores}, true
}

func countCPUCores(statPath string) int {
	snapshot, ok := readCPUSnapshot(statPath)
	if !ok {
		return 0
	}
	return snapshot.cores
}

func memoryUsage(memInfoPath string) (uint64, uint64) {
	total, available := readMemInfo(memInfoPath)
	if total == 0 {
		return 0, 0
	}
	used := total
	if available < total {
		used = total - available
	}
	return used, total
}

func readMemInfo(memInfoPath string) (uint64, uint64) {
	file, err := os.Open(memInfoPath)
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	var total uint64
	var available uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value * 1024
		case "MemAvailable":
			available = value * 1024
		}
	}
	return total, available
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return roundPercent(float64(used) * 100 / float64(total))
}

func roundPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	return float64(int(value*100+0.5)) / 100
}
