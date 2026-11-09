// openapiAssetFileDownloadLogic 单测：该 Logic 是保留脚手架桩——实际下载由自定义
// handler 直接流式输出（302/代理流），不经过 go-zero 默认 JSON 响应流程，因此
// OpenapiAssetFileDownload 恒返回 nil。测试覆盖桩行为：任意合法请求返回 nil，
// 不触碰 DB 与外部存储。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetFileDownload_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)

	l := NewOpenapiAssetFileDownloadLogic(ctx, svcCtx)
	err := l.OpenapiAssetFileDownload(&types.OpenapiAssetFileDownloadReq{FilePath: "0/assets/20260101/x.txt"})
	require.NoError(t, err)
}
