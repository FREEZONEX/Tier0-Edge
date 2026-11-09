// TASK-028 GatewayRouteCreateLogic 单元测试：
//   目标：覆盖自定义网关路由创建：必填校验、成功创建、重复创建 409、系统内置只读 403。
//   正常路径：合法请求创建成功，sys_gateway_route 落库且 system_builtin=false。
//   异常情况：缺少 routeKey/pathPattern/targetType/authPolicy 返回 400；
//             与已存在路由重复返回 409；修改系统内置路由返回 403。
package gateway

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestGatewayRouteCreate_Success(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteCreateLogic(ctx, svcCtx)

	resp, err := l.GatewayRouteCreate(validRouteReq("custom.create.ok"))
	require.NoError(t, err)
	requireOK(t, resp)

	// 落库校验：非系统路由
	routes, err := svcCtx.App.Gateway.Routes(ctx)
	require.NoError(t, err)
	found := false
	for _, route := range routes {
		if route.RouteKey == "custom.create.ok" {
			found = true
			require.False(t, route.SystemBuiltin)
		}
	}
	require.True(t, found, "route should be persisted")
}

func TestGatewayRouteCreate_MissingRequiredFields(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteCreateLogic(ctx, svcCtx)

	cases := []*types.GatewayRouteReq{
		{PathPattern: "/x/**", TargetType: "static", AuthPolicy: "login"},
		{RouteKey: "r1", TargetType: "static", AuthPolicy: "login"},
		{RouteKey: "r2", PathPattern: "/x/**", AuthPolicy: "login"},
		{RouteKey: "r3", PathPattern: "/x/**", TargetType: "static"},
	}
	for _, req := range cases {
		_, err := l.GatewayRouteCreate(req)
		requireHTTPError(t, err, 400)
	}
}

func TestGatewayRouteCreate_DuplicateReturns409(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	mustCreateRoute(t, svcCtx, ctx, "custom.create.dup")

	l := NewGatewayRouteCreateLogic(ctx, svcCtx)
	_, err := l.GatewayRouteCreate(validRouteReq("custom.create.dup"))
	requireHTTPError(t, err, 409)
}

func TestGatewayRouteCreate_SystemRouteReadonly(t *testing.T) {
	svcCtx, ctx := newGatewayEnv(t)
	l := NewGatewayRouteCreateLogic(ctx, svcCtx)

	// Seed 内置系统路由（static.assets），重新创建应拒绝
	_, err := l.GatewayRouteCreate(&types.GatewayRouteReq{
		RouteKey:    "static.assets",
		Name:        "override",
		TargetType:  "static",
		MatchType:   "prefix",
		PathPattern: "/assets/**",
		AuthPolicy:  "public",
	})
	requireHTTPError(t, err, 403)
}
