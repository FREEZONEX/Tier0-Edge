// TASK-028 unsNodeUpdateLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖控制台节点更新链路（UpdateFromConsole → 非挂载节点走 Update）。
// 正常路径：重命名节点后 name/namespace 落库更新。
// 异常情况：更新不存在的节点返回 404。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeUpdate_Rename(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "before")

	resp, err := NewUnsNodeUpdateLogic(ctx, svcCtx).UnsNodeUpdate(
		&types.UnsNodeSaveReq{NodeId: id, Name: "after", NodeType: "folder"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	require.Equal(t, "after", resp.Data.(map[string]any)["name"])

	var row repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, "after", row.Name)
	require.Equal(t, "after", row.Namespace)
	require.Equal(t, int64(1), row.UpdatedBy)
}

func TestUnsNodeUpdate_NotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsNodeUpdateLogic(ctx, svcCtx).UnsNodeUpdate(
		&types.UnsNodeSaveReq{NodeId: 999999, Name: "x", NodeType: "folder"})
	require.Equal(t, 404, httpErrorStatus(t, err))
}
