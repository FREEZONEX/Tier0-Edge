// 本文件覆盖 projectDeleteLogic.go 的 ProjectDelete：
// 正常路径：admin 删除无应用项目（软删除 + 审计记录）；
// 异常情况：项目不存在 → 404；项目下仍有应用 → 400（ErrProjectHasApps）；
//          非平台 admin / 非创建者 → 404（CheckManage 语义）。
package project

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectDeleteLogic_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	projectID := createTestProject(t, svcCtx, ctx, "delete-me")

	_, err := NewProjectDeleteLogic(ctx, svcCtx).ProjectDelete(&types.IdReq{Id: projectID})
	require.NoError(t, err)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project").
		Where("id = ? AND deleted_time = 0", projectID).Count(&count).Error)
	require.Zero(t, count, "project should be soft-deleted")
}

func TestProjectDeleteLogic_NotExist(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	_, err := NewProjectDeleteLogic(ctx, svcCtx).ProjectDelete(&types.IdReq{Id: 999999})
	require.Error(t, err)
	require.Equal(t, 404, httpErrStatus(err))
}

// TestProjectDeleteLogic_HasAppsRejected 项目下仍有活跃应用时拒绝删除（ErrProjectHasApps → 400）。
func TestProjectDeleteLogic_HasAppsRejected(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	projectID := createTestProject(t, svcCtx, ctx, "with-app")
	createTestManualApp(t, svcCtx, ctx, projectID, "app-1")

	_, err := NewProjectDeleteLogic(ctx, svcCtx).ProjectDelete(&types.IdReq{Id: projectID})
	require.Error(t, err)
	require.Equal(t, 400, httpErrStatus(err))

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project").
		Where("id = ? AND deleted_time = 0", projectID).Count(&count).Error)
	require.Equal(t, int64(1), count, "project must not be deleted while apps exist")
}

func TestProjectDeleteLogic_NonManagerRejected(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "admin-owned-2")

	_, err := NewProjectDeleteLogic(svctest.CtxWithUser(100), svcCtx).ProjectDelete(&types.IdReq{Id: projectID})
	require.Error(t, err)
	require.Equal(t, 404, httpErrStatus(err), "non-manager delete should be HTTP 404")

	var count int64
	require.NoError(t, db.Table("project").Where("id = ? AND deleted_time = 0", projectID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
