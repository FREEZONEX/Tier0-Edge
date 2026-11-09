// TASK-028 CurrentUserPasswordUpdateLogic 单元测试：
//   目标：覆盖当前用户修改密码（旧密码为空时回退用 Password 字段作为旧密码）。
//   正常路径：提供正确旧密码+新密码修改成功，且可用新密码重新登录；
//             oldPassword 为空时用 password 字段作为旧密码（前端兼容路径）。
//   异常情况：旧密码错误返回 400；ctx 无登录 Subject（UserID=0）返回 400。
package auth

import (
	"context"
	"testing"

	domainiam "backend/internal/domain/iam"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCurrentUserPasswordUpdate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserPasswordUpdateLogic(ctx, svcCtx)

	resp, err := l.CurrentUserPasswordUpdate(&types.CurrentUserPasswordReq{
		OldPassword: "tier0",
		NewPassword: "new-pass-123",
	})
	require.NoError(t, err)
	requireOK(t, resp)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "data should be map[string]any")
	require.Equal(t, true, data["updated"])

	// 旧密码失效、新密码可登录
	_, err = NewLoginLogic(ctx, svcCtx).Login(&types.LoginReq{Username: "tier0", Password: "tier0"})
	requireHTTPError(t, err, 401)
	loginResp, err := NewLoginLogic(ctx, svcCtx).Login(&types.LoginReq{Username: "tier0", Password: "new-pass-123"})
	require.NoError(t, err)
	loginData, ok := loginResp.Data.(types.LoginResp)
	require.True(t, ok)
	require.NotEmpty(t, loginData.Token)
}

func TestCurrentUserPasswordUpdate_OldPasswordFallsBackToPasswordField(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserPasswordUpdateLogic(ctx, svcCtx)

	resp, err := l.CurrentUserPasswordUpdate(&types.CurrentUserPasswordReq{
		Password:    "tier0",
		NewPassword: "fallback-pass",
	})
	require.NoError(t, err)
	requireOK(t, resp)

	// 用新密码登录验证
	loginResp, err := NewLoginLogic(ctx, svcCtx).Login(&types.LoginReq{Username: "tier0", Password: "fallback-pass"})
	require.NoError(t, err)
	loginData, ok := loginResp.Data.(types.LoginResp)
	require.True(t, ok)
	require.Equal(t, int64(1), loginData.UserId)
}

func TestCurrentUserPasswordUpdate_WrongOldPassword(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserPasswordUpdateLogic(ctx, svcCtx)

	_, err := l.CurrentUserPasswordUpdate(&types.CurrentUserPasswordReq{
		OldPassword: "wrong-old",
		NewPassword: "new-pass-123",
	})
	requireHTTPError(t, err, 400)
}

// 直接调用改密接口也必须拒绝相同密码，且不得清除初始密码标记。
func TestCurrentUserPasswordUpdate_SamePasswordPreservesResetFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  types.CurrentUserPasswordReq
	}{
		{"same", types.CurrentUserPasswordReq{OldPassword: "Initial@1", NewPassword: "Initial@1"}},
		{"trimmed", types.CurrentUserPasswordReq{OldPassword: " Initial@1 ", NewPassword: "Initial@1 "}},
		{"legacy", types.CurrentUserPasswordReq{Password: "Initial@1", NewPassword: "Initial@1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svcCtx, ctx := newSvcEnv(t)
			created, err := svcCtx.App.IAM.CreateUser(ctx, domainiam.UserSaveCommand{
				Username: "resetrequired", Password: "Initial@1", Enabled: true, IsRandomPwd: true,
				RoleList: []domainiam.UserRoleCommand{{RoleID: 2}}, UserID: 1,
			})
			require.NoError(t, err)
			userID := created["userId"].(int64)
			userRepo := repo.NewIAMRepo(ctx)
			before, err := userRepo.GetUserByID(ctx, userID)
			require.NoError(t, err)
			l := NewCurrentUserPasswordUpdateLogic(svctest.CtxWithUser(userID), svcCtx)
			_, err = l.CurrentUserPasswordUpdate(&tc.req)
			requireHTTPError(t, err, 400)
			after, err := userRepo.GetUserByID(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, before.Password, after.Password)
			require.True(t, after.IsRandomPwd)
			login, err := svcCtx.App.Auth.Login(ctx, "resetrequired", "Initial@1")
			require.NoError(t, err)
			require.True(t, login.ForceChangePassword)
			_, err = l.CurrentUserPasswordUpdate(&types.CurrentUserPasswordReq{
				OldPassword: "Initial@1", NewPassword: "Changed@2",
			})
			require.NoError(t, err)
			login, err = svcCtx.App.Auth.Login(ctx, "resetrequired", "Changed@2")
			require.NoError(t, err)
			require.False(t, login.ForceChangePassword)
		})
	}
}

func TestCurrentUserPasswordUpdate_Unauthorized(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewCurrentUserPasswordUpdateLogic(context.Background(), svcCtx)

	_, err := l.CurrentUserPasswordUpdate(&types.CurrentUserPasswordReq{
		OldPassword: "tier0",
		NewPassword: "new-pass-123",
	})
	// UserID=0 时 ChangeCurrentUserPassword 返回 ErrInvalidUser，经 logicx.Error 映射为 400
	requireHTTPError(t, err, 400)
}
