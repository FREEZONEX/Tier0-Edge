// TASK-028 UserPasswordResetLogic 单元测试：
//   目标：覆盖密码重置（bcrypt 落库、审计记录）。
//   正常路径：重置成功返回 id，sys_user_info 密码更新为 bcrypt 哈希。
//   异常情况：空密码返回 400；重置不存在的用户返回 404；重置系统内置 tier0 用户返回 403。
package iam

import (
	"testing"

	domainaudit "backend/internal/domain/audit"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestUserPasswordReset_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "alice", roleID)

	l := NewUserPasswordResetLogic(ctx, svcCtx)
	resp, err := l.UserPasswordReset(&types.UserPasswordResetReq{Id: userID, Password: "NewPass@123"})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, userID, view["id"])

	var user repo.User
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&user, "user_id = ?", userID).Error)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("NewPass@123")))

	state := domainaudit.StateFromContext(ctx)
	require.NotNil(t, state.Record)
	require.Equal(t, domainaudit.BizResetPassword, state.Record.BusinessType)
}

func TestUserPasswordReset_EmptyPasswordRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserPasswordResetLogic(ctx, svcCtx)

	_, err := l.UserPasswordReset(&types.UserPasswordResetReq{Id: 10, Password: "  "})
	requireHTTPError(t, err, 400)
}

func TestUserPasswordReset_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserPasswordResetLogic(ctx, svcCtx)

	_, err := l.UserPasswordReset(&types.UserPasswordResetReq{Id: 999999, Password: "NewPass@123"})
	requireHTTPError(t, err, 404)
}

func TestUserPasswordReset_SystemUserProtected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserPasswordResetLogic(ctx, svcCtx)

	_, err := l.UserPasswordReset(&types.UserPasswordResetReq{Id: 1, Password: "NewPass@123"})
	requireHTTPError(t, err, 403)
}
