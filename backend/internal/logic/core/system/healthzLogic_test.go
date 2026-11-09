// TASK-028 HealthzLogic 单元测试：
//
//	目标：覆盖健康检查入口。
//	正常路径：svcCtx 装配后 Healthz 返回服务名与 ok。
//	异常情况：无。
package system

import (
	"context"
	"testing"

	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthzLogic_Healthz(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.Name = "backend"

	l := NewHealthzLogic(context.Background(), svcCtx)
	resp, err := l.Healthz()
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", data["status"])
	assert.Equal(t, "backend", data["name"])
}
