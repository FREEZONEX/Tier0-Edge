// TASK-028 gateway 模块测试共享助手：
//   目标：为 5 个 route logic 文件提供统一的 ServiceContext 装配与断言工具。
//   正常路径：创建完整测试环境（独立 PG 库 + miniredis）与带登录 Subject 的 ctx，
//             经领域层创建自定义路由，以及 Envelope 解包、HTTP 错误断言。
//   异常情况：无（纯工具函数，不涉及业务异常分支）。
package gateway

import (
	"context"
	"testing"

	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newGatewayEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newGatewayEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// mustCreateRoute 直接经领域层创建一条自定义（非系统）路由，返回 routeKey。
func mustCreateRoute(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, routeKey string) string {
	t.Helper()
	err := svcCtx.App.Gateway.CreateRoute(ctx, repo.GatewayRoute{
		RouteKey:    routeKey,
		Name:        "test-route",
		TargetType:  "static",
		MatchType:   "prefix",
		PathPattern: "/t/" + routeKey + "/**",
		Methods:     []string{"GET"},
		AuthPolicy:  "login",
		Enabled:     true,
	})
	require.NoError(t, err)
	return routeKey
}

// validRouteReq 构造一条合法的自定义路由请求。
func validRouteReq(routeKey string) *types.GatewayRouteReq {
	return &types.GatewayRouteReq{
		RouteKey:    routeKey,
		Name:        "test-route",
		TargetType:  "static",
		MatchType:   "prefix",
		PathPattern: "/t/" + routeKey + "/**",
		Methods:     []string{"GET"},
		AuthPolicy:  "login",
		Enabled:     true,
	}
}

// requireOK 断言响应为成功 Envelope（Code 200）。
func requireOK(t *testing.T, resp *types.Envelope) {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
}

// requireHTTPError 断言 err 为 respx.HTTPError 且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "expected respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}
