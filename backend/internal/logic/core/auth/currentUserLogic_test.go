// TASK-028 CurrentUserLogic 单元测试：
//   目标：覆盖当前登录用户信息查询（含资源 key、UI 动作、角色、默认密码标记）。
//   正常路径：登录用户（Seed 内置 tier0 管理员）返回完整用户信息，roleCode 为 admin。
//   异常情况：ctx 无登录 Subject 时返回 401。
package auth

import (
	"context"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCurrentUser_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserLogic(ctx, svcCtx)

	resp, err := l.CurrentUser()
	require.NoError(t, err)
	data, ok := resp.Data.(types.CurrentUserResp)
	require.True(t, ok, "data should be CurrentUserResp")
	require.Equal(t, int64(1), data.UserId)
	require.Equal(t, "tier0", data.UserName)
	require.Equal(t, "admin", data.RoleCode)
	// Seed 内置用户密码为默认密码，应标记需要修改
	require.True(t, data.ForceChangePassword)
	// 管理员应有资源 key 与 UI 动作
	require.NotEmpty(t, data.ResourceKeys)
	require.NotEmpty(t, data.UiActions)
}

func TestCurrentUser_Unauthorized(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewCurrentUserLogic(context.Background(), svcCtx)
	_, err := l.CurrentUser()
	requireHTTPError(t, err, 401)
}
