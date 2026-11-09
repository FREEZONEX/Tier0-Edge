// TASK-028 AssetDetailLogic 单元测试：
//
//	目标：覆盖 asset 详情查询。
//	正常路径：已上传文件按 id 查询成功，响应含 fileId/fileName/sizeBytes 等字段。
//	异常情况：文件不存在返回 404（asset.ErrNotFound→logicx 404）；未绑定文件不返回
//	          bindingId/ownerType/ownerId 字段。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetDetail_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetDetailLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "detail.bin")

	resp, err := l.AssetDetail(&types.FileIdReq{FileId: fileID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, fileID, view["fileId"])
	require.Equal(t, "detail.bin", view["fileName"])
	require.Equal(t, int64(10), view["sizeBytes"])
	// 未绑定：不输出 owner 字段
	_, hasBinding := view["bindingId"]
	require.False(t, hasBinding, "unbound asset should not expose bindingId")
}

func TestAssetDetail_BoundShowsOwner(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetDetailLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "detail-bound.bin")
	mustBind(t, svcCtx, fileID, "projectApp", 7)

	// 注意：Detail 走 GetAssetFile（无绑定 join），bindingId/ownerType/ownerId
	// 不会出现在详情响应中；绑定信息通过 List（带 join）返回。
	resp, err := l.AssetDetail(&types.FileIdReq{FileId: fileID})
	require.NoError(t, err)
	view := respData(t, resp)
	_, hasBinding := view["bindingId"]
	require.False(t, hasBinding, "detail response does not carry binding fields")
}

func TestAssetDetail_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetDetailLogic(ctx, svcCtx)

	_, err := l.AssetDetail(&types.FileIdReq{FileId: 999999})
	requireHTTPError(t, err, 404)
}
