// TASK-028 iam 模块测试共享助手：
//
//	目标：为 16 个 logic 文件（用户/角色/资源/菜单）提供统一的场景搭建与断言工具。
//	正常路径：构造完整 ServiceContext + 登录 ctx（seed 内置 tier0 管理员）、
//	          领域层创建用户/角色（与 logic 同链路）、Envelope 解包与 HTTP 错误断言。
//	异常情况：无（纯工具函数，不涉及业务异常分支）。
package iam

import (
	"context"
	"testing"

	domainiam "backend/internal/domain/iam"
	"backend/internal/httpx"
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

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *httpx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap httpx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// mustCreateUser 经领域层创建用户并挂到指定角色，返回新用户 ID。
func mustCreateUser(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, username string, roleID int64) int64 {
	t.Helper()
	view, err := svcCtx.App.IAM.CreateUser(ctx, userSaveCmd(username, roleID))
	require.NoError(t, err)
	id, ok := view["userId"].(int64)
	require.True(t, ok, "created user view userId should be int64")
	require.Positive(t, id)
	return id
}

// userSaveCmd 构造 UserSaveCommand：密码固定为 plain（经 bcrypt 落库）。
func userSaveCmd(username string, roleID int64) domainiam.UserSaveCommand {
	return domainiam.UserSaveCommand{Username: username, Password: "plain-pass-123", RoleList: []domainiam.UserRoleCommand{{RoleID: roleID}}}
}

// mustCreateRole 经领域层创建自定义角色，返回角色 ID。
func mustCreateRole(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) int64 {
	t.Helper()
	role, err := svcCtx.App.IAM.CreateRole(ctx, name, 1)
	require.NoError(t, err)
	require.Positive(t, role.ID)
	return role.ID
}

// adminRoleID 返回 seed 内置 Admin 角色 ID。
func adminRoleID(t *testing.T, svcCtx *svc.ServiceContext) int64 {
	t.Helper()
	roles, err := svcCtx.App.IAM.Roles(context.Background())
	require.NoError(t, err)
	for _, role := range roles {
		if role.Code == repo.BuiltinRoleAdmin {
			return role.ID
		}
	}
	t.Fatal("seed should provide built-in Admin role")
	return 0
}
