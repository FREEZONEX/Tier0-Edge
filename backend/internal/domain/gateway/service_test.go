package gateway

// 测试目标：网关路由服务（service.go）。
// 覆盖的正常路径：路由 CRUD、默认值回填（matchType=prefix、timeoutMs=10000）、
// 系统内置路由只读保护。
// 覆盖的异常情况：路由不存在（ErrRouteNotFound）、路由已存在（ErrRouteExists）、
// 系统内置路由拒绝修改/删除。
// 说明：svctest 依赖 domain 包（import cycle），gateway 测试使用 testkit 等价装配。

import (
	"context"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestGateway(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	return New(ctx), ctx
}

func TestGatewayRouteCRUD(t *testing.T) {
	svc, ctx := newTestGateway(t)
	db := repo.GetCommonConn(ctx)

	// 创建
	route := repo.GatewayRoute{
		RouteKey:    "test-route",
		Name:        "Test Route",
		TargetType:  "http",
		PathPattern: "/test/*",
		TargetUrl:   "http://10.0.0.1:8080",
	}
	require.NoError(t, svc.CreateRoute(ctx, route))

	// 落库默认值
	var saved repo.GatewayRoute
	require.NoError(t, db.Where("route_key = ?", "test-route").First(&saved).Error)
	require.Equal(t, "prefix", saved.MatchType)
	require.Equal(t, 10000, saved.TimeoutMs)
	require.False(t, saved.SystemBuiltin)

	// 重复创建
	require.ErrorIs(t, svc.CreateRoute(ctx, route), ErrRouteExists)

	// 更新
	route.Name = "Test Route 2"
	route.TimeoutMs = 5000
	require.NoError(t, svc.UpdateRoute(ctx, route))
	require.NoError(t, db.Where("route_key = ?", "test-route").First(&saved).Error)
	require.Equal(t, "Test Route 2", saved.Name)
	require.Equal(t, 5000, saved.TimeoutMs)

	// 更新不存在的路由
	route2 := route
	route2.RouteKey = "missing"
	require.ErrorIs(t, svc.UpdateRoute(ctx, route2), ErrRouteNotFound)

	// 列表（含 seed 内置系统路由，只需确认 test-route 存在）
	routes, err := svc.Routes(ctx)
	require.NoError(t, err)
	found := false
	for _, r := range routes {
		if r.RouteKey == "test-route" {
			found = true
			break
		}
	}
	require.True(t, found, "test-route should be listed")

	// 删除
	require.NoError(t, svc.DeleteRoute(ctx, "test-route", 1))
	require.ErrorIs(t, svc.DeleteRoute(ctx, "test-route", 1), ErrRouteNotFound)

	// 删除不存在的路由
	require.ErrorIs(t, svc.DeleteRoute(ctx, "missing", 1), ErrRouteNotFound)
}

func TestGatewaySystemRouteReadonly(t *testing.T) {
	svc, ctx := newTestGateway(t)

	// 通过 repo upsert 落一条系统内置路由（methods 列默认 []；ID 避开 seed 内置 10000+ 段）
	sys := repo.GatewayRoute{
		ID:            999001,
		RouteKey:      "sys-route",
		Name:          "System",
		MatchType:     "prefix",
		TimeoutMs:     10000,
		SystemBuiltin: true,
	}
	require.NoError(t, svc.repo.UpsertGatewayRoute(ctx, sys))

	// 创建时命中系统内置 → 只读
	dup := repo.GatewayRoute{RouteKey: "sys-route", Name: "dup"}
	require.ErrorIs(t, svc.CreateRoute(ctx, dup), ErrSystemRouteReadonly)

	// 更新 → 只读
	require.ErrorIs(t, svc.UpdateRoute(ctx, dup), ErrSystemRouteReadonly)

	// 删除 → 只读
	require.ErrorIs(t, svc.DeleteRoute(ctx, "sys-route", 1), ErrSystemRouteReadonly)
}
