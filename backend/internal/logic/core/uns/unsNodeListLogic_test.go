// TASK-028 unsNodeListLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖节点列表查询链路（含 mounted 节点注解查询）。
// 正常路径：根下列表返回已建节点；按 keyword 过滤只返回匹配节点。
// 异常情况：空库列表返回空 list 与 total 0。
package uns

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeList_RootListContainsCreated(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	createTestFolder(t, svcCtx, ctx, "alpha")

	resp, err := NewUnsNodeListLogic(ctx, svcCtx).UnsNodeList(
		&types.UnsListReq{ParentId: 0, ParentIdSet: true})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	require.Equal(t, 1, data["total"])
	list := data["list"].([]map[string]any)
	require.Len(t, list, 1)
	require.Equal(t, "alpha", list[0]["name"])
}

func TestUnsNodeList_KeywordFilter(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	createTestFolder(t, svcCtx, ctx, "alpha")
	createTestFolder(t, svcCtx, ctx, "beta")

	resp, err := NewUnsNodeListLogic(ctx, svcCtx).UnsNodeList(
		&types.UnsListReq{Keyword: "alph"})
	require.NoError(t, err)

	data := resp.Data.(map[string]any)
	require.Equal(t, 1, data["total"])
	list := data["list"].([]map[string]any)
	require.Len(t, list, 1)
	require.Equal(t, "alpha", list[0]["name"])
}

func TestUnsNodeList_EmptyNamespace(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsNodeListLogic(ctx, svcCtx).UnsNodeList(
		&types.UnsListReq{ParentId: 0, ParentIdSet: true})
	require.NoError(t, err)

	data := resp.Data.(map[string]any)
	require.Equal(t, 0, data["total"])
	require.Empty(t, data["list"].([]map[string]any))
}
