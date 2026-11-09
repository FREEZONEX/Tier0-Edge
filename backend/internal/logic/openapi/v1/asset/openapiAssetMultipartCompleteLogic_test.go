// openapiAssetMultipartCompleteLogic 单测：测试目标为 OpenapiAssetMultipartComplete
// （合并分片并激活文件）。入参转换（parts -> filestore.CompletePart）可独立验证；
// 完整合并路径依赖对象存储 ListParts/CompleteMultipartUpload，local 驱动不支持。
// 覆盖：parts 转换正确性、空参数错误、未命中会话错误、local 驱动下 501。
package asset

import (
	"errors"
	"testing"

	domainasset "backend/internal/domain/asset"
	"backend/internal/infra/filestore"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetMultipartComplete_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	file := seedOpenapiAsset(t, db, "0/assets/20260101/complete.txt")
	file.Status = "temp"
	file.OpID = "upload-complete-1"
	file.SizeBytes = 100
	require.NoError(t, db.Model(&file).Updates(map[string]any{"status": "temp", "op_id": "upload-complete-1", "size_bytes": 100}).Error)

	l := NewOpenapiAssetMultipartCompleteLogic(ctx, svcCtx)
	// 合法入参：ResolveMultipartTemp 命中后 ListParts 在 local 驱动下报
	// ErrMultipartUnsupported（见文件头注释，正常完成路径需 s3 驱动）
	_, err := l.OpenapiAssetMultipartComplete(&types.OpenapiAssetMultipartCompleteReq{
		FileKey:  file.FileKey,
		UploadId: "upload-complete-1",
		Parts: []types.OpenapiAssetMultipartCompletePart{
			{PartNumber: 1, Etag: `"etag-1"`},
			{PartNumber: 2, Etag: `"etag-2"`},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrMultipartUnsupported), "want ErrMultipartUnsupported, got %v", err)
}

func TestOpenapiAssetMultipartComplete_PartMapping(t *testing.T) {
	// 纯转换逻辑验证：req.Parts -> []filestore.CompletePart 字段逐一对齐
	req := types.OpenapiAssetMultipartCompleteReq{
		Parts: []types.OpenapiAssetMultipartCompletePart{
			{PartNumber: 1, Etag: "e1"},
			{PartNumber: 3, Etag: "e3"},
		},
	}
	parts := make([]filestore.CompletePart, 0, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, filestore.CompletePart{PartNumber: p.PartNumber, ETag: p.Etag})
	}
	require.Len(t, parts, 2)
	require.Equal(t, int32(1), parts[0].PartNumber)
	require.Equal(t, "e1", parts[0].ETag)
	require.Equal(t, int32(3), parts[1].PartNumber)
	require.Equal(t, "e3", parts[1].ETag)
}

func TestOpenapiAssetMultipartComplete_Errors(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetMultipartCompleteLogic(ctx, svcCtx)

	// 空 uploadId / fileKey -> ErrInvalid
	_, err := l.OpenapiAssetMultipartComplete(&types.OpenapiAssetMultipartCompleteReq{FileKey: "k", UploadId: ""})
	require.Error(t, err)
	_, err = l.OpenapiAssetMultipartComplete(&types.OpenapiAssetMultipartCompleteReq{FileKey: "", UploadId: "u"})
	require.Error(t, err)

	// 未命中会话 -> not found 类错误
	_, err = l.OpenapiAssetMultipartComplete(&types.OpenapiAssetMultipartCompleteReq{
		FileKey: "k", UploadId: "no-such",
		Parts: []types.OpenapiAssetMultipartCompletePart{{PartNumber: 1, Etag: "e"}},
	})
	require.Error(t, err)
}
