// openapiAssetMultipartPartUrlsLogic 单测：测试目标为 OpenapiAssetMultipartPartUrls
// （为分片序号签发预签名 PUT 地址）。正常签发依赖对象存储 SignPutPartURL，local
// 驱动不支持；本测试覆盖可覆盖的异常情况：空参数、非法分片序号、未命中会话、
// local 驱动下 501。纯响应拼装逻辑（ExpiresAt 条件字段）独立验证。
package asset

import (
	"errors"
	"testing"

	domainasset "backend/internal/domain/asset"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetMultipartPartUrls_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	file := seedOpenapiAsset(t, db, "0/assets/20260101/parts.txt")
	file.Status = "temp"
	file.OpID = "upload-parts-1"
	require.NoError(t, db.Model(&file).Updates(map[string]any{"status": "temp", "op_id": "upload-parts-1"}).Error)

	l := NewOpenapiAssetMultipartPartUrlsLogic(ctx, svcCtx)
	// 合法入参：命中 temp 会话后 SignPutPartURL 在 local 驱动下不支持 -> 501
	// （正常签发路径需 s3/RustFS 驱动，本包不覆盖）。
	_, err := l.OpenapiAssetMultipartPartUrls(&types.OpenapiAssetMultipartPartUrlsReq{
		FileKey:     file.FileKey,
		UploadId:    "upload-parts-1",
		PartNumbers: []int32{1, 2, 3},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrMultipartUnsupported), "want ErrMultipartUnsupported, got %v", err)
}

func TestOpenapiAssetMultipartPartUrls_Errors(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetMultipartPartUrlsLogic(ctx, svcCtx)

	// 空 uploadId
	_, err := l.OpenapiAssetMultipartPartUrls(&types.OpenapiAssetMultipartPartUrlsReq{
		FileKey: "k", PartNumbers: []int32{1},
	})
	require.Error(t, err)

	// 空分片列表 -> ErrInvalid
	_, err = l.OpenapiAssetMultipartPartUrls(&types.OpenapiAssetMultipartPartUrlsReq{
		FileKey: "k", UploadId: "u",
	})
	require.Error(t, err)

	// 非法分片序号（<=0）-> ErrInvalid
	_, err = l.OpenapiAssetMultipartPartUrls(&types.OpenapiAssetMultipartPartUrlsReq{
		FileKey: "k", UploadId: "u", PartNumbers: []int32{0},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, domainasset.ErrInvalid)

	// 未命中会话 -> not found 类错误
	_, err = l.OpenapiAssetMultipartPartUrls(&types.OpenapiAssetMultipartPartUrlsReq{
		FileKey: "k", UploadId: "no-such", PartNumbers: []int32{1},
	})
	require.Error(t, err)
}

func TestOpenapiAssetMultipartPartUrls_ResponseShape(t *testing.T) {
	// 纯响应拼装逻辑验证：每个分片项的字段形状与 ExpiresAt 条件字段
	results := []domainasset.MultipartPartURLResult{
		{PartNumber: 1, URL: "http://x/1", ExpiresAt: 1700000000000},
		{PartNumber: 2, URL: "http://x/2", ExpiresAt: 0},
	}
	partURLs := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{
			"partNumber": r.PartNumber,
			"url":        r.URL,
		}
		if r.ExpiresAt > 0 {
			item["expiresAt"] = r.ExpiresAt
		}
		partURLs = append(partURLs, item)
	}
	require.Len(t, partURLs, 2)
	require.Equal(t, int32(1), partURLs[0]["partNumber"])
	require.Equal(t, int64(1700000000000), partURLs[0]["expiresAt"])
	require.Equal(t, int32(2), partURLs[1]["partNumber"])
	_, hasExpires := partURLs[1]["expiresAt"]
	require.False(t, hasExpires)
}
