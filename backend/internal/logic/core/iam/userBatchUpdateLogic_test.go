// 批量用户编辑集成测试：覆盖首页/角色批量更新、重复用户去重、系统账号保护、非法请求和事务原子性。
package iam

import (
	"testing"

	domainaudit "backend/internal/domain/audit"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUserBatchUpdate_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(1))
	roleID := adminRoleID(t, svcCtx)
	firstUserID := mustCreateUser(t, svcCtx, ctx, "batch-alice", roleID)
	secondUserID := mustCreateUser(t, svcCtx, ctx, "batch-bob", roleID)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	resp, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{firstUserID, secondUserID},
		HomePage: "/home",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 2, view["updatedCount"])
	require.Len(t, view["users"], 2)

	var configs []repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("user_id IN ?", []int64{firstUserID, secondUserID}).
		Order("user_id ASC").
		Find(&configs).Error)
	require.Len(t, configs, 2)
	require.Equal(t, "/home", configs[0].HomePage)
	require.Equal(t, "/home", configs[1].HomePage)

	auditState := domainaudit.StateFromContext(ctx)
	require.NotNil(t, auditState.Record)
	require.Equal(t, "batch", auditState.Record.ResID)
	require.Equal(t, true, auditState.Record.Detail["batch"])
	require.Equal(t, "/home", auditState.Record.Detail["requestedHomePage"])
	require.Equal(t, 2, auditState.Record.Detail["updatedCount"])
	auditedUsers, ok := auditState.Record.Detail["users"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, auditedUsers, 2)
	require.Equal(t, firstUserID, auditedUsers[0]["userId"])
	require.Equal(t, secondUserID, auditedUsers[1]["userId"])
}

func TestUserBatchUpdate_DeduplicatesUsers(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "batch-duplicate", roleID)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	resp, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{userID, userID},
		HomePage: "/home",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 1, view["updatedCount"])
	require.Len(t, view["users"], 1)
}

func TestUserBatchUpdate_ReplacesRoleAndKeepsHomePageWhenOmitted(t *testing.T) {
	svcCtx, baseCtx := newSvcEnv(t)
	ctx := svcCtx.App.Audit.WithState(baseCtx)
	adminID := adminRoleID(t, svcCtx)
	targetRoleID := mustCreateRole(t, svcCtx, ctx, "Batch target")
	firstUserID := mustCreateUser(t, svcCtx, ctx, "batch-role-alice", adminID)
	secondUserID := mustCreateUser(t, svcCtx, ctx, "batch-role-bob", adminID)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	resp, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{firstUserID, secondUserID},
		RoleList: []types.UserRoleReq{{RoleId: targetRoleID}},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 2, view["updatedCount"])
	users, ok := view["users"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, users, 2)
	require.NotContains(t, users[0], "homePage")

	type workspaceRole struct {
		UserID int64 `gorm:"column:user_id"`
		RoleID int64 `gorm:"column:role_id"`
	}
	var assignments []workspaceRole
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_workspace_user").
		Select("user_id, role_id").
		Where("user_id IN ? AND deleted_time = 0", []int64{firstUserID, secondUserID}).
		Order("user_id ASC").
		Scan(&assignments).Error)
	require.Equal(t, []workspaceRole{
		{UserID: firstUserID, RoleID: targetRoleID},
		{UserID: secondUserID, RoleID: targetRoleID},
	}, assignments)

	var configs []repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("user_id IN ?", []int64{firstUserID, secondUserID}).
		Order("user_id ASC").
		Find(&configs).Error)
	require.Len(t, configs, 2)
	require.Equal(t, "/home", configs[0].HomePage)
	require.Equal(t, "/home", configs[1].HomePage)

	auditState := domainaudit.StateFromContext(ctx)
	require.NotNil(t, auditState.Record)
	require.Equal(t, targetRoleID, auditState.Record.Detail["requestedRoleId"])
	require.NotContains(t, auditState.Record.Detail, "requestedHomePage")
}

func TestUserBatchUpdate_ResolvesHomePageAgainstReplacementRole(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	adminID := adminRoleID(t, svcCtx)
	targetRoleID := mustCreateRole(t, svcCtx, ctx, "Batch launchpad only")
	userID := mustCreateUser(t, svcCtx, ctx, "batch-role-home", adminID)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	resp, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{userID},
		HomePage: "/uns",
		RoleList: []types.UserRoleReq{{RoleId: targetRoleID}},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	users, ok := view["users"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, users, 1)
	require.Equal(t, "/launchpad", users[0]["homePage"])

	var config repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("user_id = ?", userID).First(&config).Error)
	require.Equal(t, "/launchpad", config.HomePage)
}

func TestUserBatchUpdate_FallsBackToReplacementRoleDefaultHomePage(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	adminID := adminRoleID(t, svcCtx)
	targetRoleID := mustCreateRole(t, svcCtx, ctx, "Batch UNS default")
	require.NoError(t, svcCtx.App.IAM.UpdateRole(
		ctx,
		targetRoleID,
		"",
		[]string{"resource:uns.page"},
		"/uns",
		1,
	))
	userID := mustCreateUser(t, svcCtx, ctx, "batch-role-default-home", adminID)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	resp, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{userID},
		HomePage: "/flow",
		RoleList: []types.UserRoleReq{{RoleId: targetRoleID}},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	users, ok := view["users"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, users, 1)
	require.Equal(t, "/uns", users[0]["homePage"])

	var config repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("user_id = ?", userID).First(&config).Error)
	require.Equal(t, "/uns", config.HomePage)
}

func TestUserBatchUpdate_RejectsInvalidRequest(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	logic := NewUserBatchUpdateLogic(ctx, svcCtx)

	_, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{HomePage: "/home"})
	requireHTTPError(t, err, 400)

	_, err = logic.UserBatchUpdate(&types.UserBatchUpdateReq{UserIds: []int64{1}})
	requireHTTPError(t, err, 400)

	_, err = logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds: []int64{1},
		RoleList: []types.UserRoleReq{
			{RoleId: 1},
			{RoleId: 2},
		},
	})
	requireHTTPError(t, err, 400)
}

func TestUserBatchUpdate_RejectsSystemUserForOtherActor(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)
	actorID := mustCreateUser(t, svcCtx, ctx, "batch-admin", roleID)
	actorCtx := svcCtx.App.Audit.WithState(svctest.CtxWithUser(actorID))

	var beforeCount int64
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Model(&repo.UserConfig{}).
		Where("user_id = ?", 1).
		Count(&beforeCount).Error)

	logic := NewUserBatchUpdateLogic(actorCtx, svcCtx)
	_, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{1},
		HomePage: "/uns",
	})
	requireHTTPError(t, err, 403)

	var afterCount int64
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Model(&repo.UserConfig{}).
		Where("user_id = ?", 1).
		Count(&afterCount).Error)
	require.Equal(t, beforeCount, afterCount)
}

func TestUserBatchUpdate_RejectsSystemUserRoleChangeForSelf(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	targetRoleID := mustCreateRole(t, svcCtx, ctx, "Protected role")

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	_, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{1},
		RoleList: []types.UserRoleReq{{RoleId: targetRoleID}},
	})
	requireHTTPError(t, err, 403)

	var activeRoleID int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_workspace_user").
		Where("user_id = ? AND deleted_time = 0", 1).
		Pluck("role_id", &activeRoleID).Error)
	require.Equal(t, adminRoleID(t, svcCtx), activeRoleID)
}

func TestUserBatchUpdate_IsAtomicWhenAUserDoesNotExist(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "batch-atomic", roleID)
	var before repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("user_id = ?", userID).
		First(&before).Error)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	_, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{userID, 999999},
		HomePage: "/uns",
	})
	requireHTTPError(t, err, 404)

	var after repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("user_id = ?", userID).
		First(&after).Error)
	require.Equal(t, before.HomePage, after.HomePage)
}

func TestUserBatchUpdate_IsAtomicWhenRoleDoesNotExist(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)
	userID := mustCreateUser(t, svcCtx, ctx, "batch-role-atomic", roleID)

	logic := NewUserBatchUpdateLogic(ctx, svcCtx)
	_, err := logic.UserBatchUpdate(&types.UserBatchUpdateReq{
		UserIds:  []int64{userID},
		HomePage: "/uns",
		RoleList: []types.UserRoleReq{{RoleId: 999999}},
	})
	requireHTTPError(t, err, 404)

	var config repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("user_id = ?", userID).First(&config).Error)
	require.Equal(t, "/home", config.HomePage)

	var activeRoleID int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_workspace_user").
		Where("user_id = ? AND deleted_time = 0", userID).
		Pluck("role_id", &activeRoleID).Error)
	require.Equal(t, roleID, activeRoleID)
}
