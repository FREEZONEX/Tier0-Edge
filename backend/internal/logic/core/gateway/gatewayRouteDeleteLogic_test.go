// TASK-028 GatewayRouteDeleteLogic 单元测试：
//   目标：覆盖自定义网关路由删除：成功删除、不存在 404、系统内置只读 403、未登录 401。
//   正常路径：合法 routeKey 删除成功（软删除落库）。
//   异常情况：ctx 无登录 Subject 返回 401；路由不存在返回 404；
//             系统内置路由返回 403（只读）。
package gateway

import (
	"context"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestGatewayRouteDelete_Success(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	routeKey := mustCreateRoute(t, svcCtx, ctx, "custom.delete.ok")

	l := NewGatewayRouteDeleteLogic(ctx, svcCtx)
	resp, err := l.GatewayRouteDelete(&types.RouteKeyReq{RouteKey: routeKey})
	require.NoError(t, err)
	requireOK(t, resp)

	// 软删除后列表不再包含
	routes, err := svcCtx.App.Gateway.Routes(ctx)
	require.NoError(t, err)
	for _, route := range routes {
		require.NotEqual(t, routeKey, route.RouteKey)
	}
}

func TestGatewayRouteDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteDeleteLogic(ctx, svcCtx)

	_, err := l.GatewayRouteDelete(&types.RouteKeyReq{RouteKey: "custom.delete.missing"})
	requireHTTPError(t, err, 404)
}

func TestGatewayRouteDelete_SystemRouteReadonly(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteDeleteLogic(ctx, svcCtx)

	_, err := l.GatewayRouteDelete(&types.RouteKeyReq{RouteKey: "static.assets"})
	requireHTTPError(t, err, 403)
}

func TestGatewayRouteDelete_Unauthorized(t *testing.T) {
	svcCtx, _ := newGatewayEnv(t)
	l := NewGatewayRouteDeleteLogic(context.Background(), svcCtx)

	_, err := l.GatewayRouteDelete(&types.RouteKeyReq{RouteKey: "custom.delete.unauth"})
	requireHTTPError(t, err, 401)
}
