// TASK-028 AssetUploadLogic 单元测试：
//
//	目标：覆盖单发元数据上传（无内容写入，仅落 asset_file 记录）。
//	正常路径：合法文件名上传成功，响应含 fileId/fileName/sizeBytes/sha256/storageKey。
//	异常情况：文件名为空返回 400；同名 storageKey 已有部署级（projectID=0）存活记录时
//	          直接复用旧记录返回，不新建重复行。
package asset

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetUpload_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetUploadLogic(ctx, svcCtx)

	resp, err := l.AssetUpload(&types.AssetUploadReq{
		FileName:    "upload.bin",
		ContentType: "application/octet-stream",
		Size:        100,
		Sha256:      "abc123",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	fileID, ok := view["fileId"].(int64)
	require.True(t, ok)
	require.Positive(t, fileID)
	require.Equal(t, "upload.bin", view["fileName"])
	require.Equal(t, int64(100), view["sizeBytes"])
	require.Equal(t, "abc123", view["sha256"])

	var f repo.AssetFile
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&f, fileID).Error)
	require.Equal(t, "local", f.StorageDriver)
	require.Equal(t, "private", f.Visibility)
	require.Equal(t, "temp", f.Status)
	require.Equal(t, int64(1), f.CreatedBy)
}

func TestAssetUpload_ReusesLegacyDeploymentScopeRow(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetUploadLogic(ctx, svcCtx)

	// 先上传一条部署级（projectID=0）记录
	first, err := l.AssetUpload(&types.AssetUploadReq{
		FileName:   "reuse.bin",
		StorageKey: "assets/reuse-key",
	})
	require.NoError(t, err)
	firstID := respData(t, first)["fileId"]

	// 同 storageKey 再次上传：复用旧记录，不新建
	second, err := l.AssetUpload(&types.AssetUploadReq{
		FileName:   "reuse2.bin",
		StorageKey: "assets/reuse-key",
	})
	require.NoError(t, err)
	require.Equal(t, firstID, respData(t, second)["fileId"])

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.AssetFile{}).
		Where("storage_key = ? AND deleted_time = 0", "assets/reuse-key").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestAssetUpload_EmptyFileNameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetUploadLogic(ctx, svcCtx)

	_, err := l.AssetUpload(&types.AssetUploadReq{FileName: "  "})
	requireHTTPError(t, err, 400)
}
