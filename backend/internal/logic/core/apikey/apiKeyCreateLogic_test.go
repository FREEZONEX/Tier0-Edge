// TASK-028 ApiKeyCreateLogic 单元测试：
//   目标：覆盖 API Key 创建（权限归一化、resourceKeys 默认值、明文回显）。
//   正常路径：创建成功返回 id/maskedKey/apiKey 明文；权限归一化为 read_only/data_writer；
//             resourceKeys 为空时按权限填充默认资源键。
//   异常情况：空名称返回 400；名称超长返回 400。
package apikey

import (
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestApiKeyCreate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyCreateLogic(ctx, svcCtx)

	resp, err := l.ApiKeyCreate(&types.ApiKeyCreateReq{
		Name:       "my-key",
		Permission: "read",
		UsageType:  "external",
		KeyType:    "personal",
	})
	require.NoError(t, err)
	view := respData(t, resp)

	id, ok := view["id"].(int64)
	require.True(t, ok, "api key id should be int64")
	require.Positive(t, id)
	require.Equal(t, "my-key", view["name"])
	require.Equal(t, "read_only", view["permission"], "permission should be normalized")
	require.Equal(t, "external", view["usageType"])
	require.Equal(t, "personal", view["keyType"])

	raw, ok := view["apiKey"].(string)
	require.True(t, ok, "created api key should return plaintext once")
	require.True(t, strings.HasPrefix(raw, "sk-per-"), "personal key prefix should be sk-per-")
	require.Contains(t, view["maskedKey"].(string), "...")
}

func TestApiKeyCreate_ResourceKeysDefaultedByPermission(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyCreateLogic(ctx, svcCtx)

	resp, err := l.ApiKeyCreate(&types.ApiKeyCreateReq{Name: "writer-key", Permission: "write"})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "data_writer", view["permission"])
	resourceKeys, ok := view["resourceKeys"].(string)
	require.True(t, ok)
	for _, key := range repo.APIKeyResourceKeysForPermission("write") {
		require.Contains(t, resourceKeys, key)
	}
}

func TestApiKeyCreate_EmptyNameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyCreateLogic(ctx, svcCtx)

	_, err := l.ApiKeyCreate(&types.ApiKeyCreateReq{Name: "   "})
	requireHTTPError(t, err, 400)
}

func TestApiKeyCreate_TooLongNameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyCreateLogic(ctx, svcCtx)

	_, err := l.ApiKeyCreate(&types.ApiKeyCreateReq{Name: strings.Repeat("a", 101)})
	requireHTTPError(t, err, 400)
}

func TestApiKeyCreate_CustomResourceKeys(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyCreateLogic(ctx, svcCtx)

	resp, err := l.ApiKeyCreate(&types.ApiKeyCreateReq{
		Name:         "custom-key",
		Permission:   "read",
		ResourceKeys: []string{"uns.read", "flow.read"},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "uns.read,flow.read", view["resourceKeys"])
}
