// TASK-028 ApiKeyDeleteLogic 单元测试：
//   目标：覆盖 API Key 删除（软删除落库）。
//   正常路径：删除成功返回 deleted=true，sys_api_key 软删除，列表不再包含。
//   异常情况：删除不存在的 key 返回 404。
package apikey

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestApiKeyDelete_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	keyID, _ := mustCreateAPIKey(t, svcCtx, ctx, "to-delete")

	l := NewApiKeyDeleteLogic(ctx, svcCtx)
	resp, err := l.ApiKeyDelete(&types.ApiKeyIdReq{ApiKeyId: keyID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, true, view["deleted"])

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_api_key").
		Where("id = ? AND deleted_time = 0", keyID).Count(&count).Error)
	require.Zero(t, count, "api key should be soft deleted")

	// 删除后列表不再包含。
	listResp, err := NewApiKeyListLogic(ctx, svcCtx).ApiKeyList(&types.PageReq{})
	require.NoError(t, err)
	require.Empty(t, respData(t, listResp)["list"])
}

func TestApiKeyDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyDeleteLogic(ctx, svcCtx)

	_, err := l.ApiKeyDelete(&types.ApiKeyIdReq{ApiKeyId: 999999})
	requireHTTPError(t, err, 404)
}
