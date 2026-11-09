// TASK-028 MetricsLogic 单元测试：
//
//	目标：覆盖基础指标入口。
//	正常路径：Metrics 返回固定指标字段（uptimeSeconds/httpRequests）。
//	异常情况：无。
package system

import (
	"context"
	"testing"

	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsLogic_Metrics(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	l := NewMetricsLogic(context.Background(), svcCtx)
	resp, err := l.Metrics()
	require.NoError(t, err)
	require.NotNil(t, resp)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, int64(0), data["uptimeSeconds"])
	assert.Equal(t, 0, data["httpRequests"])
}
