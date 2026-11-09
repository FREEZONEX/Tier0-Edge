// 本文件覆盖 projectAppUpdateLogic.go 的 ProjectAppUpdate：
// 正常路径：修改应用名/描述/图标/封面，manifest 同步更新；
// 异常情况：空名 → 400；同项目内重名 → 409（ErrAppNameDuplicate）；
//          应用/项目不存在 → 404；非管理者 → 404。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectAppUpdateLogic_RenameSuccess(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "update-app")
	appID := createTestManualApp(t, svcCtx, ctx, projectID, "before")

	_, err := NewProjectAppUpdateLogic(ctx, svcCtx).ProjectAppUpdate(&types.ProjectAppUpdateReq{
		ProjectId:    projectID,
		AppId:        appID,
		Name:         "after",
		Description:  "new desc",
		IconAssetId:  77,
		CoverAssetId: 88,
	})
	require.NoError(t, err)

	var app repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&app, appID).Error)
	require.Equal(t, "after", app.Name)
	require.Equal(t, int64(77), app.IconAssetID)

	var manifest map[string]any
	require.NoError(t, jsonUnmarshal(app.ManifestJSON, &manifest))
	require.Equal(t, "after", manifest["name"])
	require.Equal(t, "after", manifest["displayName"])
	require.Equal(t, "new desc", manifest["description"])
	require.Equal(t, float64(88), manifest["coverAssetId"])
}

// TestProjectAppUpdateLogic_DuplicateName 同项目内重名（DB 唯一索引 idx_project_app_project_name）→ 409。
func TestProjectAppUpdateLogic_DuplicateName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "dup-app")
	createTestManualApp(t, svcCtx, ctx, projectID, "existing")
	appID := createTestManualApp(t, svcCtx, ctx, projectID, "to-rename")

	_, err := NewProjectAppUpdateLogic(ctx, svcCtx).ProjectAppUpdate(&types.ProjectAppUpdateReq{
		ProjectId: projectID, AppId: appID, Name: "existing",
	})
	require.Error(t, err)
	require.Equal(t, 409, httpErrStatus(err), "duplicate app name should be HTTP 409")
}

func TestProjectAppUpdateLogic_Errors(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "update-errors")
	appID := createTestManualApp(t, svcCtx, ctxAdmin, projectID, "err-app")

	// 空名 → 400
	_, err := NewProjectAppUpdateLogic(ctxAdmin, svcCtx).ProjectAppUpdate(&types.ProjectAppUpdateReq{
		ProjectId: projectID, AppId: appID, Name: "  ",
	})
	require.Equal(t, 400, httpErrStatus(err))

	// 应用不存在 → 404
	_, err = NewProjectAppUpdateLogic(ctxAdmin, svcCtx).ProjectAppUpdate(&types.ProjectAppUpdateReq{
		ProjectId: projectID, AppId: 999999, Name: "ghost",
	})
	require.Equal(t, 404, httpErrStatus(err))

	// 项目不存在 → 404
	_, err = NewProjectAppUpdateLogic(ctxAdmin, svcCtx).ProjectAppUpdate(&types.ProjectAppUpdateReq{
		ProjectId: 999999, AppId: appID, Name: "ghost",
	})
	require.Equal(t, 404, httpErrStatus(err))

	// 非管理者 → 404
	_, err = NewProjectAppUpdateLogic(svctest.CtxWithUser(100), svcCtx).ProjectAppUpdate(&types.ProjectAppUpdateReq{
		ProjectId: projectID, AppId: appID, Name: "hijack",
	})
	require.Equal(t, 404, httpErrStatus(err))
}
