// TASK-028 ContainerAppDeleteLogic 单元测试：
//
//	目标：覆盖外部容器应用删除（软删除 project_app 记录）。
//	正常路径：已存在 container app 删除成功，响应 deleted=true，记录 deleted_time 非 0。
//	异常情况：app 不存在（或非 externalContainer 类型）返回 404。
package app

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestContainerAppDelete_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppDeleteLogic(ctx, svcCtx)
	item := insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "active")

	resp, err := l.ContainerAppDelete(&types.ContainerAppIdReq{AppId: item.ID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, item.ID, view["appId"])
	require.Equal(t, true, view["deleted"])

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project_app").
		Where("id = ? AND deleted_time = 0", item.ID).Count(&count).Error)
	require.Zero(t, count, "container app should be soft deleted")
}

func TestContainerAppDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppDeleteLogic(ctx, svcCtx)

	_, err := l.ContainerAppDelete(&types.ContainerAppIdReq{AppId: 999999})
	requireHTTPError(t, err, 404)
}

func TestContainerAppDelete_RejectsProjectScopedApp(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppDeleteLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "app-del-project")

	// project 作用域 app（site_type != externalContainer）不在删除范围，返回 404
	app := insertProjectApp(t, svcCtx, projectID, "inner")
	_, err := l.ContainerAppDelete(&types.ContainerAppIdReq{AppId: app.ID})
	requireHTTPError(t, err, 404)
}
