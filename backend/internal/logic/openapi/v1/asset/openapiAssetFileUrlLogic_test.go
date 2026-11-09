// openapiAssetFileUrlLogic 单测：测试目标为 OpenapiAssetFileUrl 为文件签发访问
// URL。覆盖正常路径（public 文件返回长期公开 URL；private 文件走预签名，local
// 驱动不支持则报 501）与异常情况（空 filePath、未命中、越权）。
package asset

import (
	"errors"
	"strings"
	"testing"

	domainasset "backend/internal/domain/asset"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetFileUrl_PublicFile(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	key := "0/assets/20260101/public-url.txt"
	file := seedOpenapiAsset(t, db, key)
	require.NoError(t, db.Model(&file).Update("visibility", "public").Error)

	l := NewOpenapiAssetFileUrlLogic(ctx, svcCtx)
	resp, err := l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: key})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	url, ok := data["fileUrl"].(string)
	require.True(t, ok)
	// svctest 的 PublicBaseURL 为 http://localhost，public 文件走公开能力路由
	require.True(t, strings.HasPrefix(url, "http://localhost/openapi/v1/assets/files/public?filePath="))
	// public 文件无过期时间字段
	_, hasExpires := data["expiresAt"]
	require.False(t, hasExpires)
}

func TestOpenapiAssetFileUrl_PrivateFile(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	key := "0/assets/20260101/private-url.txt"
	seedOpenapiAsset(t, db, key)

	l := NewOpenapiAssetFileUrlLogic(ctx, svcCtx)
	// local 驱动不支持签发预签名 GET URL -> ErrPresignUnsupported
	_, err := l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: key})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrPresignUnsupported), "want ErrPresignUnsupported, got %v", err)
}

func TestOpenapiAssetFileUrl_Errors(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetFileUrlLogic(ctx, svcCtx)

	// 异常情况：空 filePath
	_, err := l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: ""})
	require.Error(t, err)

	// 异常情况：未命中
	_, err = l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: "0/assets/20260101/none.txt"})
	require.Error(t, err)

	// 异常情况：越权（文件属于其他 project，调用方无访问权）
	_, err = l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: "999/assets/20260101/x.txt"})
	require.Error(t, err)
}

// 额外覆盖：expiredSec 边界传入 private 文件仍应走到驱动层（不因参数归一化报错）。
func TestOpenapiAssetFileUrl_ExpiredSecClamped(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	key := "0/assets/20260101/clamp-url.txt"
	seedOpenapiAsset(t, db, key)

	l := NewOpenapiAssetFileUrlLogic(ctx, svcCtx)
	// 超上限的 expiredSec 被 clamp 后照常走 local 驱动 -> ErrPresignUnsupported
	_, err := l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: key, ExpiredSec: 99999999})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrPresignUnsupported))

	// 负数等价默认值 -> 同样 ErrPresignUnsupported
	_, err = l.OpenapiAssetFileUrl(&types.OpenapiAssetFileUrlReq{FilePath: key, ExpiredSec: -1})
	require.Error(t, err)
	require.True(t, errors.Is(err, domainasset.ErrPresignUnsupported))
}
