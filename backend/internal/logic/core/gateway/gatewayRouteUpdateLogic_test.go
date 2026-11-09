// TASK-028 GatewayRouteUpdateLogic 单元测试：
//   目标：覆盖自定义网关路由更新：必填校验、成功更新、不存在 404、系统内置只读 403。
//   正常路径：合法请求更新成功，变更（name/pathPattern 等）落库。
//   异常情况：缺少 routeKey/pathPattern/targetType/authPolicy 返回 400；
//             更新不存在的路由返回 404；更新系统内置路由返回 403。
package gateway

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestGatewayRouteUpdate_Success(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	routeKey := mustCreateRoute(t, svcCtx, ctx, "custom.update.ok")

	req := validRouteReq(routeKey)
	req.Name = "renamed-route"
	req.PathPattern = "/t/custom.update.ok/**"
	req.Priority = 7

	l := NewGatewayRouteUpdateLogic(ctx, svcCtx)
	resp, err := l.GatewayRouteUpdate(req)
	require.NoError(t, err)
	requireOK(t, resp)

	// 落库校验更新生效
	routes, err := svcCtx.App.Gateway.Routes(ctx)
	require.NoError(t, err)
	for _, route := range routes {
		if route.RouteKey == routeKey {
			require.Equal(t, "renamed-route", route.Name)
			require.Equal(t, 7, route.Priority)
		}
	}
}

func TestGatewayRouteUpdate_MissingRequiredFields(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteUpdateLogic(ctx, svcCtx)

	cases := []*types.GatewayRouteReq{
		{PathPattern: "/x/**", TargetType: "static", AuthPolicy: "login"},
		{RouteKey: "r1", TargetType: "static", AuthPolicy: "login"},
		{RouteKey: "r2", PathPattern: "/x/**", AuthPolicy: "login"},
		{RouteKey: "r3", PathPattern: "/x/**", TargetType: "static"},
	}
	for _, req := range cases {
		_, err := l.GatewayRouteUpdate(req)
		requireHTTPError(t, err, 400)
	}
}

func TestGatewayRouteUpdate_NotFound(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteUpdateLogic(ctx, svcCtx)

	_, err := l.GatewayRouteUpdate(validRouteReq("custom.update.missing"))
	requireHTTPError(t, err, 404)
}

func TestGatewayRouteUpdate_SystemRouteReadonly(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteUpdateLogic(ctx, svcCtx)

	req := validRouteReq("static.assets")
	req.Name = "override"
	_, err := l.GatewayRouteUpdate(req)
	requireHTTPError(t, err, 403)
}
