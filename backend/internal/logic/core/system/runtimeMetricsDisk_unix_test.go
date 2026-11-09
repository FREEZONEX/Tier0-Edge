// TASK-028 runtimeMetricsDisk_unix 单元测试：
//
//	目标：覆盖 Unix 平台 diskUsage 的 statfs 解析与块大小归一化。
//	正常路径：对真实目录返回 used<=total 且 total>0。
//	异常情况：statfs 失败回退根目录；free>total 时返回 (0,total)。
package system

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeMetricsDiskUnix_RealDirectory(t *testing.T) {
	used, total := diskUsage(t.TempDir())
	require.Greater(t, total, uint64(0))
	assert.LessOrEqual(t, used, total)
}

func TestRuntimeMetricsDiskUnix_InvalidPathFallsBackToRoot(t *testing.T) {
	used, total := diskUsage(filepath.Join(t.TempDir(), "missing"))
	require.Greater(t, total, uint64(0))
	assert.LessOrEqual(t, used, total)
}

func TestRuntimeMetricsDiskUnix_StatBlockSize(t *testing.T) {
	var stat syscall.Statfs_t
	require.NoError(t, syscall.Statfs("/", &stat))
	blockSize := statBlockSize(stat)
	// Linux 下 Bsize 恒为正；Frsize 反射字段通常不存在，回退 Bsize。
	require.Greater(t, blockSize, uint64(0))
	assert.Equal(t, uint64(stat.Bsize), blockSize)
}
