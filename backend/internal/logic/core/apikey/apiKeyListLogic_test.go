// TASK-028 ApiKeyListLogic 单元测试：
//   目标：覆盖 API Key 列表（按 owner 过滤、keyType/keyword 过滤、total 统计）。
//   正常路径：返回本人全部 key；keyType=service 过滤；keyword 模糊匹配名称。
//   异常情况：无（List 为只读路径，出错仅来自 DB 故障）。
package apikey

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestApiKeyList_ReturnsOwnKeys(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	mustCreateAPIKey(t, svcCtx, ctx, "key-a")
	mustCreateAPIKey(t, svcCtx, ctx, "key-b")

	l := NewApiKeyListLogic(ctx, svcCtx)
	resp, err := l.ApiKeyList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)

	list, ok := view["list"].([]map[string]any)
	require.True(t, ok, "api key list should be []map[string]any")
	require.Equal(t, 2, len(list))
	require.Equal(t, 2, view["total"])
	for _, item := range list {
		require.Equal(t, "personal", item["keyType"])
		require.Contains(t, item["maskedKey"].(string), "...")
	}
}

func TestApiKeyList_FilterByKeyType(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	mustCreateAPIKey(t, svcCtx, ctx, "personal-key")
	_, err := svcCtx.App.APIKey.Create(ctx, "svc-key", 1, "read_only", "external", "service", nil)
	require.NoError(t, err)

	l := NewApiKeyListLogic(ctx, svcCtx)
	resp, err := l.ApiKeyList(&types.PageReq{KeyType: "service"})
	require.NoError(t, err)
	view := respData(t, resp)
	list := view["list"].([]map[string]any)
	require.Len(t, list, 1)
	require.Equal(t, "svc-key", list[0]["name"])
}

func TestApiKeyList_FilterByKeyword(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	mustCreateAPIKey(t, svcCtx, ctx, "alpha-key")
	mustCreateAPIKey(t, svcCtx, ctx, "beta-key")

	l := NewApiKeyListLogic(ctx, svcCtx)
	resp, err := l.ApiKeyList(&types.PageReq{Keyword: "beta"})
	require.NoError(t, err)
	view := respData(t, resp)
	list := view["list"].([]map[string]any)
	require.Len(t, list, 1)
	require.Equal(t, "beta-key", list[0]["name"])
}
