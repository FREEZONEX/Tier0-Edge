// 本文件覆盖 projectAppDeleteLogic.go 的 ProjectAppDelete / cleanupAppRoleBindings / cleanupDynamicApp：
// 正常路径：手动应用删除（软删除 + 角色-应用绑定清理 + 孤儿角色删除）；
//          动态应用半成品（manifest 无 app_id）删除（跳过 docker 清理，回写 deploy_error，仍软删除）；
// 异常情况：projectID/appID 非正数 → 参数错误；应用不存在 → 错误；项目不存在 → 错误。
// 未覆盖：动态应用带 app_id 时依赖真实 docker 环境与网关服务（容器/网络/卷清理、路由刷新），
//         测试环境无 docker，该分支仅记录日志不阻断删除，断言止于软删除。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestProjectAppDeleteLogic_ManualAppRemovesRoleBindings(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()

	projectID := createTestProject(t, svcCtx, ctx, "delete-app")
	appID := createTestManualApp(t, svcCtx, ctx, projectID, "to-delete")

	// 建角色并绑定该应用
	role, err := svcCtx.App.Project.UpsertRole(ctx, projectID, "viewer", "Viewer", "view only", 1)
	require.NoError(t, err)
	require.NoError(t, repo.NewProjectRepo(ctx).InsertProjectRoleApps(ctx, projectID, role.ID, []int64{appID}))

	err = NewProjectAppDeleteLogic(ctx, svcCtx).ProjectAppDelete(projectID, appID, 1)
	require.NoError(t, err)

	// 应用已软删除
	var count int64
	require.NoError(t, db.Table("project_app").
		Where("id = ? AND deleted_time = 0", appID).Count(&count).Error)
	require.Zero(t, count, "app should be soft-deleted")

	// 角色-应用绑定被清理；无其他应用引用的角色被删除（孤儿角色）
	require.NoError(t, db.Table("project_role_app").
		Where("project_id = ? AND role_id = ?", projectID, role.ID).Count(&count).Error)
	require.Zero(t, count, "role-app binding should be removed")
	require.NoError(t, db.Table("project_role").
		Where("id = ?", role.ID).Count(&count).Error)
	require.Zero(t, count, "orphan role should be deleted")
}

// TestProjectAppDeleteLogic_DynamicHalfFinished 动态应用 manifest 无 app_id（半成品部署）：
// 跳过容器清理，回写 deploy_error，应用照常软删除。
func TestProjectAppDeleteLogic_DynamicHalfFinished(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()

	projectID := createTestProject(t, svcCtx, ctx, "delete-dynamic")

	app := testkit.Insert(t, db, &repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "key-dyn",
		Name:         "half-finished",
		SiteType:     "dynamic",
		ManifestJSON: jsonRaw(`{"app_type":"dynamic","deploying":true}`),
		Status:       "deploying",
	})

	require.NoError(t, NewProjectAppDeleteLogic(ctx, svcCtx).ProjectAppDelete(projectID, app.ID, 1))

	var count int64
	require.NoError(t, db.Table("project_app").
		Where("id = ? AND deleted_time = 0", app.ID).Count(&count).Error)
	require.Zero(t, count, "half-finished dynamic app should be soft-deleted")

	// 半成品回写 deploy_error，避免前端残留 deploying 态
	var stored repo.ProjectApp
	require.NoError(t, db.Unscoped().First(&stored, app.ID).Error)
	var manifest map[string]any
	require.NoError(t, jsonUnmarshal(stored.ManifestJSON, &manifest))
	require.Equal(t, false, manifest["deploying"])
	require.Contains(t, manifest["deploy_error"], "deployment was interrupted")
}

func TestProjectAppDeleteLogic_InvalidInputs(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	// 非正数 ID → 参数错误
	require.Error(t, NewProjectAppDeleteLogic(ctx, svcCtx).ProjectAppDelete(0, 1, 1))
	require.Error(t, NewProjectAppDeleteLogic(ctx, svcCtx).ProjectAppDelete(1, 0, 1))

	// 应用不存在 → 错误
	projectID := createTestProject(t, svcCtx, ctx, "delete-missing")
	require.Error(t, NewProjectAppDeleteLogic(ctx, svcCtx).ProjectAppDelete(projectID, 999999, 1))

	// 项目不存在 → 错误
	require.Error(t, NewProjectAppDeleteLogic(ctx, svcCtx).ProjectAppDelete(999999, 1, 1))
}
