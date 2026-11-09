// openapiAssetDetailLogic 单测：测试目标为 OpenapiAssetDetail 正常返回 Envelope 与
// 错误路径（文件不存在/非法 id）。覆盖正常路径（seed 后按 fileId 查询）与异常情况
// （未命中返回 404 类错误）。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetDetail_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	seedOpenapiAsset(t, svcCtx.App.Store.CommonDB(), "p0/assets/20260101/detail.txt")

	l := NewOpenapiAssetDetailLogic(ctx, svcCtx)

	// 正常路径：命中 seed 文件，返回 Envelope{Code:200}
	resp, err := l.OpenapiAssetDetail(&types.FileIdReq{FileId: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	require.Equal(t, "success", resp.Msg)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, int64(1), data["fileId"])
	require.Equal(t, "seed.txt", data["fileName"])

	// 异常情况：不存在的 fileId -> err 非空且非 nil
	_, err = l.OpenapiAssetDetail(&types.FileIdReq{FileId: 999999})
	require.Error(t, err)

	// 异常情况：fileId <= 0
	_, err = l.OpenapiAssetDetail(&types.FileIdReq{FileId: 0})
	require.Error(t, err)
}
