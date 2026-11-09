//go:build linux

package hostinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func resourceFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadCPUTimes(t *testing.T) {
	sample, ok := readCPUTimes(resourceFixture(t, "cpu 100 20 30 200 10 5 5 5 50 10\n"))
	if !ok || sample.total != 375 || sample.idle != 210 {
		t.Fatalf("guest time counted twice or invalid totals: %+v %v", sample, ok)
	}
	for _, input := range []string{"", "cpu0 1 2 3 4", "cpu bad 2 3 4", "cpu 1 2 3"} {
		if _, ok := readCPUTimes(resourceFixture(t, input)); ok {
			t.Fatalf("accepted invalid CPU stats: %q", input)
		}
	}
}

func TestMemoryAvailableZeroAndFallback(t *testing.T) {
	for _, tc := range []struct {
		input string
		used  uint64
	}{
		{"MemTotal: 100 kB\nMemAvailable: 0 kB\nMemFree: 20 kB\nCached: 30 kB\n", 100 * 1024},
		{"MemTotal: 100 kB\nMemFree: 20 kB\nBuffers: 10 kB\nCached: 30 kB\n", 40 * 1024},
		{"MemTotal: 100 kB\nMemAvailable: 200 kB\n", 0},
	} {
		got := collectMemoryUsage(resourceFixture(t, tc.input))
		if !got.Available || got.UsedBytes != tc.used {
			t.Fatalf("wrong memory usage: %+v", got)
		}
	}
	if got := collectMemoryUsage(resourceFixture(t, "MemTotal: bad kB\n")); got.Available || got.ErrorCode == "" {
		t.Fatalf("invalid memory reported: %+v", got)
	}
}

func TestCollectLinuxResources(t *testing.T) {
	got := Collect()
	if !got.CPU.Available || got.CPU.UsagePercent < 0 || got.CPU.UsagePercent > 100 {
		t.Fatalf("CPU unavailable: %+v", got.CPU)
	}
	if !got.Memory.Available || got.Memory.UsedBytes > got.Memory.TotalBytes {
		t.Fatalf("memory unavailable: %+v", got.Memory)
	}
	if len(got.Disks) != 1 || !got.Disks[0].Available || got.Disks[0].UsedBytes > got.Disks[0].TotalBytes {
		t.Fatalf("disk unavailable: %+v", got.Disks)
	}
	t.Logf("CPU %.2f%%; memory %d/%d; root disk %d/%d", got.CPU.UsagePercent, got.Memory.UsedBytes, got.Memory.TotalBytes, got.Disks[0].UsedBytes, got.Disks[0].TotalBytes)
}
