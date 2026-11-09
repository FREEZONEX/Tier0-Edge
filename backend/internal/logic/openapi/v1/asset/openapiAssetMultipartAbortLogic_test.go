// openapiAssetMultipartAbortLogic 单测：测试目标为 OpenapiAssetMultipartAbort（中止
// 分片上传会话）。正常路径依赖对象存储分片会话（local 驱动不支持，Abort 返回
// ErrMultipartUnsupported）；本测试覆盖可覆盖的异常情况：空 fileKey/uploadId 报错、
// 未命中会话报错、local 驱动下合法入参报 501。
package asset

import (
	"errors"
	"testing"

	domainasset "backend/internal/domain/asset"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetMultipartAbort_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetMultipartAbortLogic(ctx, svcCtx)

	// 异常情况：空 uploadId -> ErrInvalid
	_, err := l.OpenapiAssetMultipartAbort(&types.OpenapiAssetMultipartAbortReq{FileKey: "k", UploadId: ""})
	require.Error(t, err)

	// 异常情况：空 fileKey -> ErrInvalid
	_, err = l.OpenapiAssetMultipartAbort(&types.OpenapiAssetMultipartAbortReq{FileKey: "", UploadId: "u"})
	require.Error(t, err)

	// 异常情况：未命中 temp 会话 -> not found 类错误
	_, err = l.OpenapiAssetMultipartAbort(&types.OpenapiAssetMultipartAbortReq{FileKey: "k", UploadId: "no-such-upload"})
	require.Error(t, err)
}

func TestOpenapiAssetMultipartAbort_LocalDriverUnsupported(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	// 直接落库一条 temp 分片会话记录（op_id 为 uploadId），使 ResolveMultipartTemp 命中，
	// 随后 AbortMultipartUpload 在 local 驱动下返回 ErrMultipartUnsupported。
	file := seedOpenapiAsset(t, db, "0/assets/20260101/abort.txt")
	file.Status = "temp"
	file.OpID = "upload-1"
	require.NoError(t, db.Model(&file).Updates(map[string]any{"status": "temp", "op_id": "upload-1"}).Error)

	l := NewOpenapiAssetMultipartAbortLogic(ctx, svcCtx)
	_, err := l.OpenapiAssetMultipartAbort(&types.OpenapiAssetMultipartAbortReq{
		FileKey:  file.FileKey,
		UploadId: "upload-1",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrMultipartUnsupported), "want ErrMultipartUnsupported, got %v", err)
}
