// TASK-028 launchpad 模块测试共享助手：
//
//	目标：提供统一的测试环境装配与场景搭建，避免各测试文件重复代码。
//	正常路径：直插测试用户；经 repo 层创建 Project / App / Role / Member
//	          （绕过 tenantpg lifecycle 的真实 CREATE DATABASE），与 logic 同一链路；
//	          以及 Envelope 解包、HTTP 错误断言。
//	异常情况：无（纯工具函数，不涉及业务异常分支）。
package launchpad

import (
	"context"
	"encoding/json"
	"testing"

	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newSvcEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newSvcEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// respData 解包成功响应的 Data 为 map[string]any。
func respData(t *testing.T, resp *types.Envelope) map[string]any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	m, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	return m
}

// respDataAny 解包成功响应的 Data 为原始类型（用于 Data 为切片/struct 的场景）。
func respDataAny(t *testing.T, resp *types.Envelope) any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	return resp.Data
}

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// mustCreateUser 直插一个测试用户（seed 只内置 user 1）。
func mustCreateUser(t *testing.T, svcCtx *svc.ServiceContext, userID int64, name string) {
	t.Helper()
	u := repo.User{ID: userID, UserName: name, NickName: name, Password: "x", Status: 1}
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&u).Error)
}

// mustCreateProject 经 repo 层创建项目并把 userID 作为 owner/成员，返回项目 ID。
// 注意：不走 svcCtx.App.Project.Create，避免 tenantpg lifecycle 真实 CREATE DATABASE。
func mustCreateProject(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string, userID int64) int64 {
	t.Helper()
	item, err := repo.NewProjectRepo(ctx).CreateProject(ctx, repo.Project{
		Name:        name,
		Description: "test project",
		OwnerUserID: userID,
	})
	require.NoError(t, err)
	_, err = repo.NewProjectRepo(ctx).UpsertProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: item.ID,
		UserID:    userID,
		RoleIDs:   nil,
	})
	require.NoError(t, err)
	return item.ID
}

// mustAddMember 经 repo 层添加项目成员（可指定角色），返回成员 ID。
func mustAddMember(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, projectID, userID int64, roleIDs []int64) int64 {
	t.Helper()
	member, err := repo.NewProjectRepo(ctx).UpsertProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: projectID,
		UserID:    userID,
		RoleIDs:   roleIDs,
	})
	require.NoError(t, err)
	require.Positive(t, member.ID)
	return member.ID
}

// mustCreateRole 经 repo 层创建项目角色，返回角色 ID。
func mustCreateRole(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, projectID int64, roleKey, name string) int64 {
	t.Helper()
	role, err := repo.NewProjectRepo(ctx).UpsertProjectRole(ctx, repo.ProjectRole{
		ProjectID:   projectID,
		RoleKey:     roleKey,
		Name:        name,
		Description: "test role",
	})
	require.NoError(t, err)
	require.Positive(t, role.ID)
	return role.ID
}

// mustCreateApp 经 repo 层创建应用（active 状态），返回应用 ID。
func mustCreateApp(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, projectID int64, name string, userID int64) int64 {
	t.Helper()
	item, err := repo.NewProjectRepo(ctx).CreateProjectApp(ctx, repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "app_" + name,
		Name:         name,
		SiteType:     "sameOriginStatic",
		ManifestJSON: json.RawMessage(`{"app_type":"web"}`),
		Status:       "active",
	})
	require.NoError(t, err)
	require.Positive(t, item.ID)
	return item.ID
}

// bindAppToRole 直接把应用绑定到角色（repo 层，与领域层同一链路）。
func bindAppToRole(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, projectID, roleID, appID int64) {
	t.Helper()
	require.NoError(t, repo.NewProjectRepo(ctx).InsertProjectRoleApps(ctx, projectID, roleID, []int64{appID}))
}
