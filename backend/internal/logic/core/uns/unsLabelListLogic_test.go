// TASK-028 unsLabelListLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖标签列表链路（Labels → 全量 active 标签）。
// 正常路径：经 UnsLabelCrudLogic 创建两个标签后列表 total=2 且按 id 排序返回。
// 异常情况：空库返回空 list 与 total 0。
package uns

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsLabelList_ReturnsCreatedLabels(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	crud := NewUnsLabelCrudLogic(ctx, svcCtx)
	for _, name := range []string{"label-a", "label-b"} {
		_, err := crud.Create(&types.UnsLabelReq{Name: name})
		require.NoError(t, err)
	}

	resp, err := NewUnsLabelListLogic(ctx, svcCtx).UnsLabelList(&types.PageReq{})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	require.Equal(t, 2, data["total"])
	list := data["list"].([]any)
	require.Len(t, list, 2)
	require.Equal(t, "label-a", list[0].(map[string]any)["name"])
	require.Equal(t, "label-b", list[1].(map[string]any)["name"])
}

func TestUnsLabelList_Empty(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsLabelListLogic(ctx, svcCtx).UnsLabelList(&types.PageReq{})
	require.NoError(t, err)

	data := resp.Data.(map[string]any)
	require.Equal(t, 0, data["total"])
	require.Empty(t, data["list"].([]any))
}
