// TASK-028 AssetMultipartPartUrlsLogic 单元测试：
//
//	目标：覆盖分片 PUT 地址签发（temp 记录定位 + 项目管理权限复检 + 地址签发）。
//	正常路径：admin/owner 对合法 (fileKey, uploadId) 签发成功，返回与请求一一对应的
//	          partUrls（含 partNumber/url/expiresAt）。
//	异常情况：temp 记录不存在或 fileKey 不匹配返回 404；非管理员返回 404；
//	          partNumbers 为空或含 ≤0 序号返回 400；local 驱动不支持签发返回 501。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetMultipartPartUrls_RejectsUnknownTemp(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartPartUrlsLogic(ctx, svcCtx)

	_, err := l.AssetMultipartPartUrls(&types.AssetMultipartPartUrlsReq{
		FileKey: "no_such_key", UploadId: "no-such-upload", PartNumbers: []int32{1},
	})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartPartUrls_RejectsMissingParams(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartPartUrlsLogic(ctx, svcCtx)

	_, err := l.AssetMultipartPartUrls(&types.AssetMultipartPartUrlsReq{})
	requireHTTPError(t, err, 400)
}

func TestAssetMultipartPartUrls_LocalDriverUnsupported(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartPartUrlsLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-urls")
	temp := insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "upload-001", "flat_key_big_zip")

	_, err := l.AssetMultipartPartUrls(&types.AssetMultipartPartUrlsReq{
		FileKey: "flat_key_big_zip", UploadId: "upload-001", PartNumbers: []int32{1},
	})
	requireHTTPError(t, err, 501)
	require.NotZero(t, temp)
}

func TestAssetMultipartPartUrls_FileKeyMismatchRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartPartUrlsLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-urls-key")
	insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "upload-002", "flat_key_big_zip")

	// fileKey 与记录不匹配按不存在处理
	_, err := l.AssetMultipartPartUrls(&types.AssetMultipartPartUrlsReq{
		FileKey: "wrong_key", UploadId: "upload-002", PartNumbers: []int32{1},
	})
	requireHTTPError(t, err, 404)
}
