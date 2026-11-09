// openapiAssetMultipartInitLogic 单测：测试目标为 OpenapiAssetMultipartInit（分片上传
// 会话创建）。正常路径依赖对象存储 CreateMultipartUpload，local 驱动不支持（返回
// ErrMultipartUnsupported）；本测试覆盖可覆盖的异常情况：入参校验失败与 local 驱动
// 下的 501。会话落库成功路径需 s3/RustFS 驱动，不覆盖（见文件头注释）。
package asset

import (
	"errors"
	"testing"

	domainasset "backend/internal/domain/asset"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetMultipartInit_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetMultipartInitLogic(ctx, svcCtx)

	// 异常情况：空文件名 -> ErrInvalid
	_, err := l.OpenapiAssetMultipartInit(&types.OpenapiAssetMultipartInitReq{})
	require.Error(t, err)
	require.ErrorIs(t, err, domainasset.ErrInvalid)

	// 异常情况：非法扩展名
	_, err = l.OpenapiAssetMultipartInit(&types.OpenapiAssetMultipartInitReq{FileName: "x.php", Size: 100})
	require.Error(t, err)

	// 异常情况：size <= 0
	_, err = l.OpenapiAssetMultipartInit(&types.OpenapiAssetMultipartInitReq{FileName: "a.bin", Size: 0})
	require.Error(t, err)

	// 异常情况：非法 visibility
	_, err = l.OpenapiAssetMultipartInit(&types.OpenapiAssetMultipartInitReq{
		FileName:   "a.bin",
		Size:       100,
		Visibility: "bogus",
	})
	require.Error(t, err)

	// 异常情况：appInstanceId 超 64 字符
	_, err = l.OpenapiAssetMultipartInit(&types.OpenapiAssetMultipartInitReq{
		FileName:      "a.bin",
		Size:          100,
		AppInstanceId: string(make([]byte, 65)),
	})
	require.Error(t, err)
}

func TestOpenapiAssetMultipartInit_LocalDriverUnsupported(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetMultipartInitLogic(ctx, svcCtx)

	// 合法入参：local 驱动 CreateMultipartUpload 返回 ErrMultipartUnsupported
	// （正常签发路径需 s3/RustFS 驱动，本包不覆盖）。
	_, err := l.OpenapiAssetMultipartInit(&types.OpenapiAssetMultipartInitReq{
		FileName: "big.bin",
		Size:     100,
		PartSize: 1024 * 1024 * 5,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrMultipartUnsupported), "want ErrMultipartUnsupported, got %v", err)
}
