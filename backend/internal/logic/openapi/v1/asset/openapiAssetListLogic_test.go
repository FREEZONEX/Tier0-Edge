// openapiAssetListLogic 单测：测试目标为 OpenapiAssetList（openapi 全量文件列表，
// ownerType/ownerID 恒为空）。覆盖正常路径（空库返回空列表；seed 后返回全部记录）
// 与入参无关的错误路径（无）。注意该接口不按用户过滤，返回库中全部未删除记录。
package asset

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/testkit"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetList_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()

	l := NewOpenapiAssetListLogic(ctx, svcCtx)

	// 正常路径（空库）：list 为空数组、total=0
	resp, err := l.OpenapiAssetList(&types.PageReq{})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, 0, data["total"])
	require.Empty(t, data["list"])

	// seed 两条记录后：total=2
	testkit.Insert(t, db, &repo.AssetFile{
		FileKey:      "k1",
		OriginalName: "a.txt",
		StorageKey:   "0/assets/20260101/a.txt",
		Status:       "active",
	})
	testkit.Insert(t, db, &repo.AssetFile{
		FileKey:      "k2",
		OriginalName: "b.txt",
		StorageKey:   "0/assets/20260101/b.txt",
		Status:       "active",
	})
	resp, err = l.OpenapiAssetList(&types.PageReq{})
	require.NoError(t, err)
	data = resp.Data.(map[string]any)
	require.Equal(t, 2, data["total"])
	require.Len(t, data["list"], 2)
}
