// TASK-028 ContainerAppVisibilityLogic 单元测试：
//
//	目标：覆盖外部容器应用启停（enabled→active，disabled→stopped）。
//	正常路径：enabled=true 置 active，enabled=false 置 stopped，响应回显 appId/enabled。
//	异常情况：app 不存在返回 404。
package app

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestContainerAppVisibility_Enable(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppVisibilityLogic(ctx, svcCtx)
	item := insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "stopped")

	resp, err := l.ContainerAppVisibility(&types.ContainerAppVisibilityReq{AppId: item.ID, Enabled: true})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, item.ID, view["appId"])
	require.Equal(t, true, view["enabled"])

	var updated repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&updated, item.ID).Error)
	require.Equal(t, "active", updated.Status)
}

func TestContainerAppVisibility_Disable(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppVisibilityLogic(ctx, svcCtx)
	item := insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "active")

	resp, err := l.ContainerAppVisibility(&types.ContainerAppVisibilityReq{AppId: item.ID, Enabled: false})
	require.NoError(t, err)
	require.Equal(t, false, respData(t, resp)["enabled"])

	var updated repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&updated, item.ID).Error)
	require.Equal(t, "stopped", updated.Status)
}

func TestContainerAppVisibility_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppVisibilityLogic(ctx, svcCtx)

	_, err := l.ContainerAppVisibility(&types.ContainerAppVisibilityReq{AppId: 999999, Enabled: true})
	requireHTTPError(t, err, 404)
}
