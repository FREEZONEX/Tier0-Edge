// openapiAssetFilePublicDownloadLogic 单测：该 Logic 是保留脚手架桩——公开下载由
// 自定义 handler 直接流式输出，不经过 go-zero 默认 JSON 响应流程，因此
// OpenapiAssetFilePublicDownload 恒返回 nil。测试覆盖桩行为：任意合法请求返回 nil，
// 不触碰 DB 与外部存储。
package assetpublic

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetFilePublicDownload_Behavior(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiAssetFilePublicDownloadLogic(ctx, svcCtx)
	err := l.OpenapiAssetFilePublicDownload(&types.OpenapiAssetFileDownloadReq{FilePath: "0/assets/20260101/x.txt"})
	require.NoError(t, err)

	// 桩不感知请求内容，任意入参同样返回 nil
	err = l.OpenapiAssetFilePublicDownload(&types.OpenapiAssetFileDownloadReq{})
	require.NoError(t, err)
}
