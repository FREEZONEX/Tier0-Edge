// TASK-028 GatewayRouteTestLogic 单元测试：
//   目标：覆盖路由连通性测试接口（阶段1 为配置测试占位实现，不发起真实请求）。
//   正常路径：任意 routeKey 返回 reachable=true 与 phase1 说明 note。
//   异常情况：无（占位实现不依赖外部服务、不校验参数）。
package gateway

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestGatewayRouteTest_ReturnsPhase1Stub(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteTestLogic(ctx, svcCtx)

	resp, err := l.GatewayRouteTest(&types.RouteKeyReq{RouteKey: "static.assets"})
	require.NoError(t, err)
	requireOK(t, resp)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "data should be map[string]any")
	require.Equal(t, "static.assets", data["routeKey"])
	require.Equal(t, true, data["reachable"])
	require.Equal(t, "phase1 config test only", data["note"])
}
