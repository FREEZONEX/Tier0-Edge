// TASK-028 GatewayRouteListLogic 单元测试：
//   目标：覆盖网关路由列表查询（含 Seed 内置系统路由与自定义路由）。
//   正常路径：返回 list + total，系统内置路由包含在内，自定义路由可见。
//   异常情况：无（列表接口不校验登录态，也不对外部服务有依赖）。
package gateway

import (
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestGatewayRouteList_ContainsSeedAndCustomRoutes(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	mustCreateRoute(t, svcCtx, ctx, "custom.list.visible")

	l := NewGatewayRouteListLogic(ctx, svcCtx)
	resp, err := l.GatewayRouteList()
	require.NoError(t, err)
	requireOK(t, resp)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "data should be map[string]any")
	list, ok := data["list"].([]repo.GatewayRoute)
	require.True(t, ok, "list should be []repo.GatewayRoute")
	total, ok := data["total"].(int)
	require.True(t, ok, "total should be int")
	require.Equal(t, len(list), total)

	keys := map[string]bool{}
	for _, route := range list {
		keys[route.RouteKey] = true
	}
	// Seed 内置路由存在
	require.True(t, keys["static.assets"], "seed builtin route should be listed")
	// 自定义路由存在
	require.True(t, keys["custom.list.visible"], "custom route should be listed")
}
