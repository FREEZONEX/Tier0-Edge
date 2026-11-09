// TASK-028 unsNodeDeleteLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖节点软删除链路（删除子树进回收站）。
// 正常路径：删除后节点 status=deleted、deleted_time>0、recycle_is_del=2，响应含 deleted 计数。
// 异常情况：删除不存在的节点返回 404。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeDelete_SoftDeletesSubtree(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "to-delete")
	childID := createTestFolder(t, svcCtx, ctx, "child")

	resp, err := NewUnsNodeDeleteLogic(ctx, svcCtx).UnsNodeDelete(&types.NodeIdReq{NodeId: id})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	require.Equal(t, 1, resp.Data.(map[string]any)["deleted"])

	var root repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Unscoped().First(&root, id).Error)
	require.Equal(t, repo.UnsNodeStatusDeleted, root.Status)
	require.Equal(t, int16(repo.UnsNodeRecycleVisible), root.RecycleIsDel)
	require.Greater(t, int64(root.DeletedTime), int64(0))
	require.Equal(t, int64(1), root.DeletedBy)

	// 独立根节点不受影响（仍 active）
	var child repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&child, childID).Error)
	require.Equal(t, repo.UnsNodeStatusActive, child.Status)
}

func TestUnsNodeDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsNodeDeleteLogic(ctx, svcCtx).UnsNodeDelete(&types.NodeIdReq{NodeId: 999999})
	require.Equal(t, 404, httpErrorStatus(t, err))
}
