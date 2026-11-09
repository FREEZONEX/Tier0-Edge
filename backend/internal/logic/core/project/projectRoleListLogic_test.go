// 本文件覆盖 projectRoleListLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际角色列表能力在领域层 svcCtx.App.Project.ListRoles，这里补测其 DB 行为
// （正常路径：角色带应用 → Apps 填充 appId/appName 且去重；无应用角色 → Apps 为空）。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectRoleListLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectRoleListLogic(ctx, svcCtx).ProjectRoleList(&types.ProjectAppReq{ProjectId: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectRoleDomain_ListRolesWithApps(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	projectID := createTestProject(t, svcCtx, ctx, "list-roles")
	appID := createTestManualApp(t, svcCtx, ctx, projectID, "role-app")

	// 一个角色绑定应用，一个角色无绑定
	withApp, err := svcCtx.App.Project.UpsertRole(ctx, projectID, "with-app", "WithApp", "has app", 1)
	require.NoError(t, err)
	_, err = svcCtx.App.Project.UpsertRole(ctx, projectID, "empty-role", "EmptyRole", "no app", 1)
	require.NoError(t, err)
	require.NoError(t, repo.NewProjectRepo(ctx).InsertProjectRoleApps(ctx, projectID, withApp.ID, []int64{appID}))

	roles, err := svcCtx.App.Project.ListRoles(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, roles, 2)

	byKey := map[string]repo.ProjectRole{}
	for _, r := range roles {
		byKey[r.RoleKey] = r
	}
	require.Len(t, byKey["with-app"].Apps, 1)
	require.Equal(t, appID, byKey["with-app"].Apps[0].AppID)
	require.Equal(t, "role-app", byKey["with-app"].Apps[0].AppName)
	require.Empty(t, byKey["empty-role"].Apps)
}
