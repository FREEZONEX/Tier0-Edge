// 本文件覆盖 projectAppManualCreateLogic.go 的 ProjectAppManualCreate：
// 正常路径：proxy 模式（容器名+端口 → 生成网关路由与转发前缀）、external 模式（合法 URL 直存）；
// 异常情况：空名/非法容器名/非法 external URL → 400；项目不存在 → 404；非管理者 → 404。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectAppManualCreate_ProxyMode(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "manual-proxy")

	resp, err := NewProjectAppManualCreateLogic(ctx, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId:      projectID,
		Name:           "web-app",
		Description:    "manual web",
		AccessMode:     "proxy",
		ContainerName:  "web-container",
		ContainerPort:  3000,
		OpenInPlatform: true,
	})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	appID := data["appId"].(int64)
	require.Positive(t, appID)
	require.Equal(t, "success", data["deployStatus"])
	require.Equal(t, true, data["manual"])

	var app repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&app, appID).Error)
	require.Equal(t, "manualExternal", app.SiteType)
	require.Equal(t, "active", app.Status)
	require.Equal(t, "/app-proxy/projects/"+itoa64(projectID)+"/apps/"+itoa64(appID)+"/", app.EntryURL)
	require.Equal(t, "project.manual."+itoa64(projectID)+"."+itoa64(appID), app.GatewayRouteKey)

	// proxy 模式应写入网关路由表（表名 sys_gateway_route）
	var routeCount int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_gateway_route").
		Where("route_key = ?", app.GatewayRouteKey).Count(&routeCount).Error)
	require.Equal(t, int64(1), routeCount)

	// manifest 记录容器名与端口
	var manifest map[string]any
	require.NoError(t, jsonUnmarshal(app.ManifestJSON, &manifest))
	require.Equal(t, "manual", manifest["app_type"])
	require.Equal(t, "web-container", manifest["containerName"])
	require.Equal(t, float64(3000), manifest["containerPort"])
	require.Equal(t, true, manifest["openInPlatform"])
}

func TestProjectAppManualCreate_ExternalMode(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "manual-external")

	resp, err := NewProjectAppManualCreateLogic(ctx, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId:  projectID,
		Name:       "external-app",
		AccessMode: "external",
		EntryUrl:   "https://example.com/dashboard",
	})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, "success", data["deployStatus"])

	var app repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&app, data["appId"].(int64)).Error)
	require.Equal(t, "https://example.com/dashboard", app.EntryURL)
	require.Empty(t, app.GatewayRouteKey, "external 模式不生成转发路由")
}

// TestProjectAppManualCreate_InvalidInputs 覆盖校验失败与权限失败路径。
func TestProjectAppManualCreate_InvalidInputs(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "manual-invalid")

	// 空名 → 400
	_, err := NewProjectAppManualCreateLogic(ctxAdmin, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId: projectID, Name: "  ",
	})
	require.Equal(t, 400, httpErrStatus(err))

	// proxy 模式非法容器名（localhost / 空）→ 400
	for _, container := range []string{"", "localhost"} {
		_, err := NewProjectAppManualCreateLogic(ctxAdmin, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
			ProjectId: projectID, Name: "bad-container", AccessMode: "proxy", ContainerName: container,
		})
		require.Equal(t, 400, httpErrStatus(err), "container %q should be rejected", container)
	}

	// external 模式非法 URL → 400
	_, err = NewProjectAppManualCreateLogic(ctxAdmin, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId: projectID, Name: "bad-url", AccessMode: "external", EntryUrl: "ftp://nope",
	})
	require.Equal(t, 400, httpErrStatus(err))

	// 项目不存在 → 404（需给合法参数，先通过参数校验走到项目查询）
	_, err = NewProjectAppManualCreateLogic(ctxAdmin, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId: 999999, Name: "ghost-app", AccessMode: "external", EntryUrl: "https://example.com/x",
	})
	require.Equal(t, 404, httpErrStatus(err))

	// 非管理者 → 404（补合法 proxy 参数，先通过参数校验走到权限校验）
	_, err = NewProjectAppManualCreateLogic(svctest.CtxWithUser(100), svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId: projectID, Name: "hijack-app", AccessMode: "proxy", ContainerName: "hijack", ContainerPort: 3000,
	})
	require.Equal(t, 404, httpErrStatus(err))
}

// TestProjectAppManualCreate_DuplicateName 同名手动应用应返回 409 project.appNameDuplicate，
// 而不是 raw common.duplicateResource。
func TestProjectAppManualCreate_DuplicateName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "manual-dup")

	_, err := NewProjectAppManualCreateLogic(ctx, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId:  projectID,
		Name:       "same-name",
		AccessMode: "external",
		EntryUrl:   "https://example.com/x",
	})
	require.NoError(t, err)

	_, err = NewProjectAppManualCreateLogic(ctx, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId:  projectID,
		Name:       "same-name",
		AccessMode: "external",
		EntryUrl:   "https://example.com/y",
	})
	require.Equal(t, 409, httpErrStatus(err))
}
