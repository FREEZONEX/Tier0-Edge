// TASK-028 UserCreateLogic 单元测试：
//   目标：覆盖用户创建（角色绑定、bcrypt 落库、审计记录、Envelope 响应）。
//   正常路径：合法请求创建成功，sys_user_info 落库且密码为 bcrypt 哈希，审计 state 写入。
//   异常情况：缺少用户名/密码/角色返回 400；用户名重复返回 409。
package iam

import (
	"testing"

	domainaudit "backend/internal/domain/audit"
	domainiam "backend/internal/domain/iam"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// 覆盖默认关闭、显式开关、登录状态，以及错误旧密码不会清除改密标记。
func TestUserCreate_PasswordResetPreference(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default_off", true: "on"}[enabled], func(t *testing.T) {
			svcCtx, ctx := newSvcEnv(t)
			roleID := adminRoleID(t, svcCtx)
			resp, err := NewUserCreateLogic(ctx, svcCtx).UserCreate(&types.UserSaveReq{
				Username: "resetpreference", Password: "Initial@1", Enabled: true,
				ForceChangePassword: enabled, RoleList: []types.UserRoleReq{{RoleId: roleID}},
			})
			require.NoError(t, err)
			userID := respData(t, resp)["userId"].(int64)
			user, err := repo.NewIAMRepo(ctx).GetUserByID(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, enabled, user.IsRandomPwd)
			login, err := svcCtx.App.Auth.Login(ctx, "resetpreference", "Initial@1")
			require.NoError(t, err)
			require.Equal(t, enabled, login.ForceChangePassword)
			_, _, _, pending, err := svcCtx.App.Auth.CurrentUser(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, enabled, pending)
			err = svcCtx.App.IAM.ChangeCurrentUserPassword(ctx, domainiam.CurrentUserPasswordCommand{
				UserID: userID, OldPassword: "wrong", NewPassword: "Changed@2",
			})
			require.Error(t, err)
			_, _, _, pending, err = svcCtx.App.Auth.CurrentUser(ctx, userID)
			require.NoError(t, err)
			require.Equal(t, enabled, pending)
			require.NoError(t, svcCtx.App.IAM.ChangeCurrentUserPassword(ctx, domainiam.CurrentUserPasswordCommand{
				UserID: userID, OldPassword: "Initial@1", NewPassword: "Changed@2",
			}))
			_, _, _, pending, err = svcCtx.App.Auth.CurrentUser(ctx, userID)
			require.NoError(t, err)
			require.False(t, pending)
			login, err = svcCtx.App.Auth.Login(ctx, "resetpreference", "Changed@2")
			require.NoError(t, err)
			require.False(t, login.ForceChangePassword)
		})
	}
}

func TestUserCreate_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)

	l := NewUserCreateLogic(ctx, svcCtx)
	resp, err := l.UserCreate(&types.UserSaveReq{
		Username:  "alice",
		Password:  "Secret@123",
		FirstName: "Alice",
		Email:     "alice@example.com",
		Phone:     "13800000000",
		Enabled:   true,
		RoleList:  []types.UserRoleReq{{RoleId: roleID, RoleName: "Admin"}},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	userID, ok := view["userId"].(int64)
	require.True(t, ok, "created user id should be int64")
	require.Positive(t, userID)
	require.Equal(t, "alice", view["userName"])
	require.Equal(t, int64(1), view["status"])

	var user repo.User
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&user, "user_id = ?", userID).Error)
	require.Equal(t, "alice", user.UserName)
	require.False(t, user.IsRandomPwd, "未指定时默认不要求重置密码")
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("Secret@123")),
		"password should be stored as bcrypt hash")

	// 审计只在 ctx state 中记录，落库走 HTTP 中间件（logic 单测不覆盖落库）。
	state := domainaudit.StateFromContext(ctx)
	require.NotNil(t, state)
	require.NotNil(t, state.Record)
	require.Equal(t, domainaudit.BizCreate, state.Record.BusinessType)
	require.Equal(t, "alice", state.Record.ResName)
}

func TestUserCreate_DisabledDefault(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)

	l := NewUserCreateLogic(ctx, svcCtx)
	resp, err := l.UserCreate(&types.UserSaveReq{Username: "bob", Password: "pw-123", RoleList: []types.UserRoleReq{{RoleId: roleID}}})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, int64(0), view["status"])
}

func TestUserCreate_MissingUsernameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserCreateLogic(ctx, svcCtx)

	_, err := l.UserCreate(&types.UserSaveReq{Password: "pw-123", RoleList: []types.UserRoleReq{{RoleId: 1}}})
	requireHTTPError(t, err, 400)
}

func TestUserCreate_MissingPasswordRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserCreateLogic(ctx, svcCtx)

	_, err := l.UserCreate(&types.UserSaveReq{Username: "carol", RoleList: []types.UserRoleReq{{RoleId: 1}}})
	requireHTTPError(t, err, 400)
}

func TestUserCreate_MissingRoleRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserCreateLogic(ctx, svcCtx)

	_, err := l.UserCreate(&types.UserSaveReq{Username: "carol", Password: "pw-123"})
	requireHTTPError(t, err, 400)
}

func TestUserCreate_DuplicateUsernameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)
	mustCreateUser(t, svcCtx, ctx, "dave", roleID)

	l := NewUserCreateLogic(ctx, svcCtx)
	_, err := l.UserCreate(&types.UserSaveReq{Username: "dave", Password: "pw-123", RoleList: []types.UserRoleReq{{RoleId: roleID}}})
	requireHTTPError(t, err, 409)
}
