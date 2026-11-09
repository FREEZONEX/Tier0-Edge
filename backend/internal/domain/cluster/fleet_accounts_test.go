// fleet_accounts_test.go 覆盖集群节点账户分配（fleet_accounts.go）：
// DB 路径：FleetNodeAccounts 查询、ReplaceFleetNodeAccounts 成功分配
//
//	（含自定义可分配用户）、修订冲突与不可分配用户错误映射。
//
// 纯函数：fleetAccountAssignmentResponse 响应构造。
package cluster

import (
	"testing"
	"time"

	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetNodeAccounts_Invalid(t *testing.T) {
	_, s := newTestCluster(t)
	_, err := s.FleetNodeAccounts(backgroundCtx(), "  ")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.FleetNodeAccounts(backgroundCtx(), "fn_nope")
	require.Error(t, err)
}

func TestFleetNodeAccounts_Empty(t *testing.T) {
	store, s := newTestCluster(t)
	ctx := backgroundCtx()
	created, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_acc1", Name: "edge-1", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	_ = created
	resp, err := s.FleetNodeAccounts(ctx, "fn_acc1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["revision"])
	assert.Empty(t, resp["centerUserIDs"])
	assert.Empty(t, resp["accounts"])
	_ = store
}

func TestReplaceFleetNodeAccounts_Conflict(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_acc2", Name: "edge-2", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	// 期望修订号 1，但当前为 0 → 冲突。
	_, err = s.ReplaceFleetNodeAccounts(ctx, FleetAccountAssignmentCommand{
		FleetNodeID: "fn_acc2", CenterUserIDs: []int64{1}, ExpectedRevision: 1, UserID: 1,
	})
	require.ErrorIs(t, err, ErrAccountRevisionConflict)
}

func TestReplaceFleetNodeAccounts_NotAssignable(t *testing.T) {
	_, s := newTestCluster(t)
	ctx := backgroundCtx()
	_, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_acc3", Name: "edge-3", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	// tier0 内置管理员不可分配。
	_, err = s.ReplaceFleetNodeAccounts(ctx, FleetAccountAssignmentCommand{
		FleetNodeID: "fn_acc3", CenterUserIDs: []int64{1}, ExpectedRevision: 0, UserID: 1,
	})
	require.ErrorIs(t, err, ErrAccountNotAssignable)
}

func TestReplaceFleetNodeAccounts_Success(t *testing.T) {
	store, s := newTestCluster(t)
	ctx := backgroundCtx()
	created, err := s.fleetRepo.CreateNodeWithConnection(ctx, repo.CreateFleetNodeInput{
		FleetNodeID: "fn_acc4", Name: "edge-4", CenterType: "enterprise", ActorID: 1, BranchLimit: 3,
	})
	require.NoError(t, err)
	_ = created

	db := store.CommonDB()
	now := time.Now().UTC()
	// 可分配用户：user_id >= PersonalIDStart，用户名非 tier0，角色非 owner。
	require.NoError(t, db.Exec(`INSERT INTO sys_user_info
		(user_id, user_name, nick_name, password, status, created_by, updated_by, created_time, updated_time)
		VALUES (1000000001, 'ops-user', 'Ops', 'x', 1, 1, 1, ?, ?)`, now, now).Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_workspace_user
		(workspace_id, user_id, role_code, role_id, created_by, updated_by, created_time, updated_time)
		VALUES (1, 1000000001, 'operator', 2, 1, 1, ?, ?)`, now, now).Error)

	resp, err := s.ReplaceFleetNodeAccounts(ctx, FleetAccountAssignmentCommand{
		FleetNodeID: "fn_acc4", CenterUserIDs: []int64{1000000001}, ExpectedRevision: 0, UserID: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, true, resp["changed"])
	assert.Equal(t, int64(1), resp["revision"])
	assert.Equal(t, []string{"1000000001"}, resp["centerUserIDs"])
	accounts := resp["accounts"].([]map[string]any)
	require.Len(t, accounts, 1)
	assert.Equal(t, "ops-user", accounts[0]["userName"])
	assert.Equal(t, int64(1), accounts[0]["assignmentRevision"])

	// 再次查询返回同一快照。
	resp, err = s.FleetNodeAccounts(ctx, "fn_acc4")
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp["revision"])
	assert.Len(t, resp["accounts"].([]map[string]any), 1)
}

func TestFleetAccountAssignmentResponse(t *testing.T) {
	snapshot := repo.FleetAccountAssignmentSnapshot{
		NodeID: "n1", FleetNodeID: "fn_x", Revision: 3,
		Accounts: []repo.FleetAssignedAccount{{
			CenterUserID: 42, UserName: "u", NickName: "nick", Email: "e@x.com", Phone: "123",
			UserStatus: 1, RoleCode: "operator", AssignmentRevision: 3, AssignedBy: 1, AssignedAt: 9,
		}},
	}
	resp := fleetAccountAssignmentResponse(snapshot, true)
	assert.Equal(t, "fn_x", resp["fleetNodeID"])
	assert.Equal(t, true, resp["changed"])
	assert.Equal(t, int64(3), resp["revision"])
	assert.Equal(t, []string{"42"}, resp["centerUserIDs"])
	accounts := resp["accounts"].([]map[string]any)
	require.Len(t, accounts, 1)
	assert.Equal(t, "u", accounts[0]["userName"])
	assert.Equal(t, "42", accounts[0]["centerUserID"])
}
