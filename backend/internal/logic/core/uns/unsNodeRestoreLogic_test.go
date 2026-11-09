// TASK-028 unsNodeRestoreLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖回收站恢复链路（无命名空间冲突时 confirm=false 直接恢复）。
// 正常路径：软删除节点恢复为 active（deleted_time=0、status=active、recycle_is_del=0），
// 响应含 restored 计数。
// 异常情况：恢复不存在的节点返回 404。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeRestore_AfterDelete(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "revive-me")
	softDeleteTestNode(t, svcCtx, ctx, id)

	resp, err := NewUnsNodeRestoreLogic(ctx, svcCtx).UnsNodeRestore(
		&types.UnsRestoreReq{NodeId: id, Confirm: false})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	require.Equal(t, 1, resp.Data.(map[string]any)["restored"])

	var row repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, repo.UnsNodeStatusActive, row.Status)
	require.Equal(t, int16(0), row.RecycleIsDel)
	require.Equal(t, int64(0), int64(row.DeletedTime))
}

func TestUnsNodeRestore_NotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsNodeRestoreLogic(ctx, svcCtx).UnsNodeRestore(
		&types.UnsRestoreReq{NodeId: 999999})
	require.Equal(t, 404, httpErrorStatus(t, err))
}
