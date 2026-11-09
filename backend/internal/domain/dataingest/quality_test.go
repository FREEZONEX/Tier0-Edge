package dataingest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 测试目标：接入层只构造借用视图并保留双时间观察；内容指纹由启用
// duplicate 的质量 worker 在节点过滤后计算，避免所有节点同步付费。
func TestQualityObservations_TimesAndBorrowedPayload(t *testing.T) {
	now := time.Now().UTC()
	source := now.Add(-time.Second)
	left := Record{
		UnsID: 1, Time: now, IngestedAt: now, SourceTime: source, SourceTimeSet: true,
		RawValues: map[string]string{"b": "2", "a": "1"}, Fields: []Field{{Name: "a", Type: "double"}},
		Replayed: true, SourceQuality: "Bad",
	}
	observations := qualityObservations([]Record{left})
	require.Len(t, observations, 1)
	require.Zero(t, observations[0].CanonicalHash)
	require.Equal(t, source, observations[0].SourceTime)
	require.Equal(t, now, observations[0].IngestedAt)
	require.True(t, observations[0].Replayed)
	require.Equal(t, int64(3), observations[0].QualityCode)
	require.Equal(t, left.Fields, observations[0].Fields)
	require.Equal(t, left.RawValues, observations[0].RawValues)
}
