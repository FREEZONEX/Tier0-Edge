// 本文件覆盖 projectAppStatusLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际状态流转能力在领域层 svcCtx.App.Project.UpdateAppStatus，这里补测其 DB 行为
// （正常路径：active↔stopped 流转并落库；异常：非法状态 → ErrInvalid、应用/项目不存在 → ErrNotFound、
//  非管理者 → ErrNotFound）。
package project

import (
	"errors"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectAppStatusLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectAppStatusLogic(ctx, svcCtx).ProjectAppStatus(&types.ProjectAppStatusReq{
		ProjectId: 1, AppId: 1, Status: "stopped",
	})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectAppDomain_StatusFlow(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "status-app")
	appID := createTestManualApp(t, svcCtx, ctxAdmin, projectID, "status-target")

	// active → stopped
	_, err := svcCtx.App.Project.UpdateAppStatus(ctxAdmin, projectID, appID, "stopped", 1)
	require.NoError(t, err)
	var app repo.ProjectApp
	require.NoError(t, db.First(&app, appID).Error)
	require.Equal(t, "stopped", app.Status)

	// stopped → active（恢复）
	_, err = svcCtx.App.Project.UpdateAppStatus(ctxAdmin, projectID, appID, "active", 1)
	require.NoError(t, err)
	require.NoError(t, db.First(&app, appID).Error)
	require.Equal(t, "active", app.Status)

	// 非法状态 → ErrInvalid
	_, err = svcCtx.App.Project.UpdateAppStatus(ctxAdmin, projectID, appID, "bogus", 1)
	require.True(t, errors.Is(err, domainproject.ErrInvalid))

	// 应用不存在 → ErrNotFound
	_, err = svcCtx.App.Project.UpdateAppStatus(ctxAdmin, projectID, 999999, "stopped", 1)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))

	// 项目不存在 → ErrNotFound
	_, err = svcCtx.App.Project.UpdateAppStatus(ctxAdmin, 999999, appID, "stopped", 1)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))

	// 非管理者 → ErrNotFound
	_, err = svcCtx.App.Project.UpdateAppStatus(svctest.CtxWithUser(100), projectID, appID, "stopped", 100)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}
