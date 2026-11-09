// TASK-028 AssetListLogic 单元测试：
//
//	目标：覆盖按 (ownerType, ownerId) 的 asset 列表查询。
//	正常路径：仅返回绑定到该 owner 的文件，total 与实际数量一致；owner 过滤条件为空时
//	          返回全部存活文件。
//	异常情况：无（领域层 List 不校验入参，无错误分支）。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetList_ByOwner(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetListLogic(ctx, svcCtx)

	mine := insertAsset(t, svcCtx, "mine.bin")
	other := insertAsset(t, svcCtx, "other.bin")
	mustBind(t, svcCtx, mine, "projectApp", 11)
	mustBind(t, svcCtx, other, "projectApp", 22)

	resp, err := l.AssetList(&types.AssetListReq{OwnerType: "projectApp", OwnerId: 11})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 1, view["total"])
	list, ok := view["list"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, list, 1)
	require.Equal(t, mine, list[0]["fileId"])
	require.Equal(t, int64(11), list[0]["ownerId"])
}

func TestAssetList_NoFilterReturnsAll(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetListLogic(ctx, svcCtx)

	insertAsset(t, svcCtx, "a.bin")
	insertAsset(t, svcCtx, "b.bin")

	resp, err := l.AssetList(&types.AssetListReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 2, view["total"])
}

func TestAssetList_EmptyOwnerReturnsEmpty(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetListLogic(ctx, svcCtx)

	resp, err := l.AssetList(&types.AssetListReq{OwnerType: "projectApp", OwnerId: 999})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 0, view["total"])
	list, ok := view["list"].([]map[string]any)
	require.True(t, ok)
	require.Empty(t, list)
}
