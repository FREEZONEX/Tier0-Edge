// TASK-028 unsRecycleListLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖回收站列表链路（RecycleOnly 过滤已软删除节点）。
// 正常路径：软删除的节点出现在回收站列表；未删除节点不出现。
// 异常情况：空回收站返回空 list 与 total 0。
package uns

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsRecycleList_AfterDelete(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	activeID := createTestFolder(t, svcCtx, ctx, "active-one")
	deletedID := createTestFolder(t, svcCtx, ctx, "gone-one")
	softDeleteTestNode(t, svcCtx, ctx, deletedID)

	resp, err := NewUnsRecycleListLogic(ctx, svcCtx).UnsRecycleList(&types.PageReq{})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	require.Equal(t, 1, data["total"])
	list := data["list"].([]map[string]any)
	require.Len(t, list, 1)
	require.Equal(t, deletedID, list[0]["id"].(int64))
	require.NotEqual(t, activeID, list[0]["id"].(int64))
}

func TestUnsRecycleList_Empty(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsRecycleListLogic(ctx, svcCtx).UnsRecycleList(&types.PageReq{})
	require.NoError(t, err)

	data := resp.Data.(map[string]any)
	require.Equal(t, 0, data["total"])
	require.Empty(t, data["list"].([]map[string]any))
}
