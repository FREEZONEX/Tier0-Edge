// TASK-028 AssetDownloadLogic 单元测试：
//
//	目标：覆盖 asset 下载信息查询。
//	正常路径：已上传文件返回 backend-stream 下载元信息（mode/driver/storageKey）。
//	异常情况：文件不存在返回 404。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetDownload_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetDownloadLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "dl.bin")

	resp, err := l.AssetDownload(&types.FileIdReq{FileId: fileID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, fileID, view["fileId"])
	dl, ok := view["download"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "backend-stream", dl["mode"])
	require.Equal(t, "local", dl["driver"])
	require.NotEmpty(t, dl["storageKey"])
}

func TestAssetDownload_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetDownloadLogic(ctx, svcCtx)

	_, err := l.AssetDownload(&types.FileIdReq{FileId: 999999})
	requireHTTPError(t, err, 404)
}
