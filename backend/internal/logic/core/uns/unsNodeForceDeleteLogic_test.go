// TASK-028 unsNodeForceDeleteLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖强制删除（物理清理）链路。
// 正常路径：回收站内节点强制删除后 status=finalDeleted、recycle_is_del=1。
// 异常情况：未删除（active）节点强制删除返回 404（ErrNotInRecycle）。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeForceDelete_AfterDelete(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "trash-me")
	softDeleteTestNode(t, svcCtx, ctx, id)

	resp, err := NewUnsNodeForceDeleteLogic(ctx, svcCtx).UnsNodeForceDelete(
		&types.UnsForceDeleteReq{NodeId: id, DeleteFlow: false})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	require.Equal(t, 1, resp.Data.(map[string]any)["forceDeleted"])

	var row repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Unscoped().First(&row, id).Error)
	require.Equal(t, repo.UnsNodeStatusFinal, row.Status)
	require.Equal(t, int16(repo.UnsNodeRecycleHidden), row.RecycleIsDel)
}

func TestUnsNodeForceDelete_ActiveNodeRejected(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "still-active")

	_, err := NewUnsNodeForceDeleteLogic(ctx, svcCtx).UnsNodeForceDelete(
		&types.UnsForceDeleteReq{NodeId: id})
	require.Equal(t, 404, httpErrorStatus(t, err))
}
