// TASK-028 UserUpdateLogic 单元测试：
//
//	目标：覆盖用户更新（基础信息、角色重绑、homePage 解析回填、审计记录）。
//	正常路径：更新成功落库；homePage 为合法路径时写入 user_config 并回填 data。
//	异常情况：更新不存在的用户返回 404；tier0 系统用户被他人修改返回 403。
package iam

import (
	"encoding/json"
	"errors"
	"testing"

	domainaudit "backend/internal/domain/audit"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserUpdate_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "alice", roleID)

	l := NewUserUpdateLogic(ctx, svcCtx)
	resp, err := l.UserUpdate(&types.UserUpdateReq{
		Id:        userID,
		Username:  "alice2",
		FirstName: "Alice New",
		Email:     "alice2@example.com",
		Enabled:   true,
		RoleList:  []types.UserRoleReq{{RoleId: roleID, RoleName: "Admin"}},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "alice2", view["userName"])
	require.Equal(t, "Alice New", view["nickName"])

	var user repo.User
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&user, "user_id = ?", userID).Error)
	require.Equal(t, "alice2", user.UserName)
	require.Equal(t, "Alice New", user.NickName)

	state := domainaudit.StateFromContext(ctx)
	require.NotNil(t, state.Record)
	require.Equal(t, domainaudit.BizUpdate, state.Record.BusinessType)
	require.Equal(t, "alice2", state.Record.ResName)
}

func TestUserUpdate_HomePagePersisted(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "bob", roleID)

	l := NewUserUpdateLogic(ctx, svcCtx)
	resp, err := l.UserUpdate(&types.UserUpdateReq{Id: userID, HomePage: "/home"})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "/home", view["homePage"])

	var cfg repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("user_id = ?", userID).First(&cfg).Error)
	require.Equal(t, "/home", cfg.HomePage)
}

func TestUserUpdate_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserUpdateLogic(ctx, svcCtx)

	_, err := l.UserUpdate(&types.UserUpdateReq{Id: 999999, Username: "ghost"})
	requireHTTPError(t, err, 404)
}

func TestUserUpdate_SystemUserProtected(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	adminID := int64(1)

	// 以非本人身份修改系统内置 tier0 用户（ActorID=1 与 UserID=1 相同则视为本人更新，
	// 故用一个全新 ctx 携带不同的 ActorID 模拟他人操作）。
	otherCtx := svctest.CtxWithUser(2)
	l := NewUserUpdateLogic(otherCtx, svcCtx)
	_, err := l.UserUpdate(&types.UserUpdateReq{Id: adminID, Username: "hacked"})
	requireHTTPError(t, err, 403)
}

func TestUserUpdate_StatusRecordsOneAction(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.App.IAM.BindAudit(svcCtx.App.Audit)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	id := mustCreateUser(t, svcCtx, ctx, "status_audit", roleID)
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.User{}).Where("user_id = ?", id).Update("status", 1).Error)
	_, err := NewUserUpdateLogic(ctx, svcCtx).UserUpdate(&types.UserUpdateReq{Id: id, Username: "status_audit", Enabled: false, RoleList: []types.UserRoleReq{{RoleId: roleID, RoleName: "Admin"}}})
	require.NoError(t, err)
	state := domainaudit.StateFromContext(ctx)
	require.NotNil(t, state.Record)
	require.Equal(t, domainaudit.BizDisable, state.Record.BusinessType)
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.OutboxEvent{}).Where("event_type = ?", domainaudit.OutboxEventType).Count(&count).Error)
	require.EqualValues(t, 1, count, "已提交的状态独立记账")
	require.True(t, state.Suppressed, "成功请求不重复记录通用 Update")
}

func TestUserUpdateStatusSurvivesHomePageFailure(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.App.IAM.BindAudit(svcCtx.App.Audit)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	id := mustCreateUser(t, svcCtx, ctx, "partial_status", roleID)
	db := svcCtx.App.Store.CommonDB()
	require.NoError(t, db.Model(&repo.User{}).Where("user_id = ?", id).Update("status", 1).Error)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_homepage", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_user_config" {
			tx.AddError(errors.New("homepage write unavailable"))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("fail_homepage") })
	_, err := NewUserUpdateLogic(ctx, svcCtx).UserUpdate(&types.UserUpdateReq{Id: id, Enabled: false, HomePage: "/home"})
	require.Error(t, err)
	var user repo.User
	require.NoError(t, db.First(&user, "user_id = ?", id).Error)
	require.Zero(t, user.Status)
	var event repo.OutboxEvent
	require.NoError(t, db.Where("event_type = ?", domainaudit.OutboxEventType).First(&event).Error)
	var payload struct{ Record repo.AuditLog }
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	require.Equal(t, domainaudit.BizDisable, payload.Record.BusinessType)
	require.EqualValues(t, 200, payload.Record.Code)
	require.False(t, domainaudit.StateFromContext(ctx).Suppressed)
	require.Nil(t, domainaudit.StateFromContext(ctx).Record)
}
