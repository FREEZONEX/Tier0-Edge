// TASK-028 LoginLogic 单元测试：
//
//	目标：覆盖用户名密码登录（成功签发 token、失败返回 401）。
//	正常路径：Seed 内置 tier0/tier0 登录成功，返回 token、用户信息、角色与资源；
//	          登录成功后 session 建立（后续 CurrentUser 可用）。
//	异常情况：密码错误、用户不存在均返回 401。
//
// 登录日志补充用例：登录成功/失败各落一条 sys_user_login_log（异步落库，Eventually 轮询）。
package auth

import (
	"encoding/json"
	"testing"
	"time"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"

	"backend/internal/types"
)

func TestLogin_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLoginLogic(ctx, svcCtx)

	resp, err := l.Login(&types.LoginReq{Username: "tier0", Password: "tier0"})
	require.NoError(t, err)
	data, ok := resp.Data.(types.LoginResp)
	require.True(t, ok, "data should be LoginResp")
	require.NotEmpty(t, data.Token)
	require.Equal(t, int64(1), data.UserId)
	require.Equal(t, "tier0", data.UserName)
	require.Equal(t, "admin", data.RoleCode)
	// Seed 用户为默认密码，ForceChangePassword 应为 true
	require.True(t, data.ForceChangePassword)
	require.NotEmpty(t, data.ResourceKeys)
}

func TestLogin_WrongPassword(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLoginLogic(ctx, svcCtx)

	_, err := l.Login(&types.LoginReq{Username: "tier0", Password: "wrong"})
	requireHTTPError(t, err, 401)
}

func TestLogin_UnknownUser(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLoginLogic(ctx, svcCtx)

	_, err := l.Login(&types.LoginReq{Username: "ghost", Password: "whatever"})
	requireHTTPError(t, err, 401)
}

// 登录成功应异步落一条 sys_user_login_log：user_id=1、code=200。
func TestLogin_LoginLogSuccess(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLoginLogic(ctx, svcCtx)

	_, err := l.Login(&types.LoginReq{Username: "tier0", Password: "tier0"})
	require.NoError(t, err)

	loginLogRepo := repo.NewUserLoginLogRepo(ctx)
	require.Eventually(t, func() bool {
		items, total, listErr := loginLogRepo.ListUserLoginLogs(ctx, repo.UserLoginLogFilter{UserName: "tier0", PageNo: 1, PageSize: 10})
		if listErr != nil || total == 0 {
			return false
		}
		for _, item := range items {
			if item.Code == 200 && item.UserID == 1 {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "expected a successful login log row")
}

// 登录失败应异步落一条 sys_user_login_log：user_id=0、user_name=输入账号、code=401、msg 记录原因。
func TestLogin_LoginLogFailure(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLoginLogic(ctx, svcCtx)

	_, err := l.Login(&types.LoginReq{Username: "ghost", Password: "whatever"})
	requireHTTPError(t, err, 401)

	loginLogRepo := repo.NewUserLoginLogRepo(ctx)
	require.Eventually(t, func() bool {
		items, total, listErr := loginLogRepo.ListUserLoginLogs(ctx, repo.UserLoginLogFilter{UserName: "ghost", PageNo: 1, PageSize: 10})
		if listErr != nil || total == 0 {
			return false
		}
		for _, item := range items {
			if item.Code == 401 && item.UserID == 0 && item.Msg != "" {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "expected a failed login log row")
}

// The native-to-web handoff uses exactly the existing Launchpad authorization.
// A granted ticket is consumed even if access is revoked before redemption.
func TestMiniEntryRechecksAppAccess(t *testing.T) {
	svc, ctx := newSvcEnv(t)
	login, err := svc.App.Auth.Login(ctx, "tier0", "tier0")
	require.NoError(t, err)
	require.True(t, login.ForceChangePassword, "optional reminder must not bypass app permission checks")
	projects := repo.NewProjectRepo(ctx)
	project, err := projects.CreateProject(ctx, repo.Project{Name: "mini-entry-test", OwnerUserID: 1})
	require.NoError(t, err)
	role, err := projects.UpsertProjectRole(ctx, repo.ProjectRole{ProjectID: project.ID, RoleKey: "viewer", Name: "Viewer"})
	require.NoError(t, err)
	_, err = projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{ProjectID: project.ID, UserID: 1, RoleIDs: []int64{role.ID}})
	require.NoError(t, err)
	app, err := projects.CreateProjectApp(ctx, repo.ProjectApp{ProjectID: project.ID, AppKey: "mini-entry-app", Name: "App", SiteType: "sameOriginStatic", ManifestJSON: json.RawMessage(`{"app_type":"web"}`), Status: "active"})
	require.NoError(t, err)
	require.NoError(t, projects.InsertProjectRoleApps(ctx, project.ID, role.ID, []int64{app.ID}))
	logic := NewMiniEntryLogic(ctx, svc)
	request := &types.MiniEntryTicketReq{ProjectId: project.ID, AppId: app.ID}
	response, err := logic.Ticket(request, login.Token)
	require.NoError(t, err)
	ticket := response.Data.(map[string]any)["ticket"].(string)
	grant, _, err := logic.Exchange(ticket)
	require.NoError(t, err)
	require.Equal(t, login.Token, grant.Token)
	_, _, err = logic.Exchange(ticket)
	requireHTTPError(t, err, 401)
	// Clear just the short issuance throttle in this isolated test Redis.
	require.NoError(t, svc.App.Cache.Del(ctx, "auth:mini-entry-rate:1"))
	response, err = logic.Ticket(request, login.Token)
	require.NoError(t, err)
	ticket = response.Data.(map[string]any)["ticket"].(string)
	_, err = projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{ProjectID: project.ID, UserID: 1, RoleIDs: nil})
	require.NoError(t, err)
	_, _, err = logic.Exchange(ticket)
	requireHTTPError(t, err, 403)
	_, _, err = logic.Exchange(ticket)
	requireHTTPError(t, err, 401)
	_, err = logic.Ticket(request, login.Token)
	requireHTTPError(t, err, 403)
}
