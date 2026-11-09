// TASK-028 convert 单元测试：
//   目标：覆盖 GatewayRouteReq -> repo.GatewayRoute 字段映射（纯函数，无 DB 依赖）。
//   正常路径：全部字段逐一映射（含 slice/bool/int 字段）。
//   异常情况：无（纯赋值，无校验分支）。
package gateway

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestToRepoRoute_AllFields(t *testing.T) {
	req := &types.GatewayRouteReq{
		RouteKey:    "route.a",
		Name:        "Route A",
		Description: "desc",
		TargetType:  "reverseProxy",
		MatchType:   "prefix",
		PathPattern: "/a/**",
		Methods:     []string{"GET", "POST"},
		TargetUrl:   "http://localhost:8080",
		TargetPath:  "/upstream",
		StripPrefix: true,
		RewritePath: "/rewritten",
		AuthPolicy:  "login",
		ResourceKey: "flow.read",
		TimeoutMs:   5000,
		Priority:    3,
		Enabled:     true,
	}
	got := toRepoRoute(req)
	require.Equal(t, repo.GatewayRoute{
		RouteKey:    "route.a",
		Name:        "Route A",
		Description: "desc",
		TargetType:  "reverseProxy",
		MatchType:   "prefix",
		PathPattern: "/a/**",
		Methods:     []string{"GET", "POST"},
		TargetUrl:   "http://localhost:8080",
		TargetPath:  "/upstream",
		StripPrefix: true,
		RewritePath: "/rewritten",
		AuthPolicy:  "login",
		ResourceKey: "flow.read",
		TimeoutMs:   5000,
		Priority:    3,
		Enabled:     true,
	}, got)
}

func TestToRepoRoute_ZeroValues(t *testing.T) {
	got := toRepoRoute(&types.GatewayRouteReq{})
	require.Equal(t, repo.GatewayRoute{}, got)
}
