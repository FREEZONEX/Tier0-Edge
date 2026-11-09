// openapiAssetFileUploadLogic 单测：测试目标为 OpenapiAssetFileUpload（预签名 POST
// 上传签发）。正常路径依赖存储驱动支持 SignPostURL；svctest 装配的 local 驱动不支持，
// 因此正常路径无法覆盖（文件头注释说明），本测试覆盖可覆盖的异常情况：
// 入参校验失败（空文件名/非法扩展名/非法 visibility）与 local 驱动下的
// ErrPresignUnsupported（501）。逻辑层不落库，直接返回错误。
package asset

import (
	"errors"
	"testing"

	domainasset "backend/internal/domain/asset"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetFileUpload_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetFileUploadLogic(ctx, svcCtx)

	// 异常情况：空文件名 -> ErrInvalid -> logicx.Error 包装后非 nil
	_, err := l.OpenapiAssetFileUpload(&types.OpenapiAssetFileUploadReq{})
	require.Error(t, err)
	require.ErrorIs(t, err, domainasset.ErrInvalid)

	// 异常情况：非法扩展名（黑名单）
	_, err = l.OpenapiAssetFileUpload(&types.OpenapiAssetFileUploadReq{FileName: "evil.html", Size: 10})
	require.Error(t, err)

	// 异常情况：申报大小 <= 0
	_, err = l.OpenapiAssetFileUpload(&types.OpenapiAssetFileUploadReq{FileName: "a.txt", Size: 0})
	require.Error(t, err)

	// 异常情况：非法 visibility
	_, err = l.OpenapiAssetFileUpload(&types.OpenapiAssetFileUploadReq{
		FileName:   "a.txt",
		Size:       10,
		Visibility: "bogus",
	})
	require.Error(t, err)
}

func TestOpenapiAssetFileUpload_LocalDriverUnsupported(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetFileUploadLogic(ctx, svcCtx)

	// svctest 使用 local 驱动：合法入参走到 SignPostURL 即返回 ErrPresignUnsupported。
	// 正常签发路径需要 s3/RustFS 驱动，本包不覆盖（见文件头注释）。
	_, err := l.OpenapiAssetFileUpload(&types.OpenapiAssetFileUploadReq{
		FileName: "hello.txt",
		Size:     10,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrPresignUnsupported), "want ErrPresignUnsupported, got %v", err)
}
