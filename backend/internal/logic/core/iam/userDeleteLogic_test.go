// TASK-028 UserDeleteLogic 单元测试：
//   目标：覆盖用户删除（软删除落库、项目转移给系统 owner、审计记录）。
//   正常路径：删除成功返回 id，sys_user_info 软删除（deleted_time 非 0），审计 state 写入。
//   异常情况：删除不存在的用户返回 404；删除系统内置 tier0 用户返回 403。
package iam

import (
	"testing"

	domainaudit "backend/internal/domain/audit"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUserDelete_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "alice", roleID)

	l := NewUserDeleteLogic(ctx, svcCtx)
	resp, err := l.UserDelete(&types.IdReq{Id: userID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, userID, view["id"])

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_user_info").
		Where("user_id = ? AND deleted_time = 0", userID).Count(&count).Error)
	require.Zero(t, count, "user should be soft deleted")

	state := domainaudit.StateFromContext(ctx)
	require.NotNil(t, state.Record)
	require.Equal(t, domainaudit.BizDelete, state.Record.BusinessType)
	require.Equal(t, "alice", state.Record.ResName)
}

func TestUserDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserDeleteLogic(ctx, svcCtx)

	_, err := l.UserDelete(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

func TestUserDelete_SystemUserProtected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserDeleteLogic(ctx, svcCtx)

	_, err := l.UserDelete(&types.IdReq{Id: 1})
	requireHTTPError(t, err, 403)
}
