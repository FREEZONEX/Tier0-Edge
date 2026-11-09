// TASK-028 ApiKeyUpdateLogic 单元测试：
//   目标：覆盖 API Key 更新（名称/权限更新、resourceKeys 按权限重算）。
//   正常路径：更新成功返回新名称与归一化权限，落库生效。
//   异常情况：更新不存在的 key 返回 404；名称超长返回 400。
package apikey

import (
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestApiKeyUpdate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	keyID, _ := mustCreateAPIKey(t, svcCtx, ctx, "old-name")

	l := NewApiKeyUpdateLogic(ctx, svcCtx)
	resp, err := l.ApiKeyUpdate(&types.ApiKeyUpdateReq{
		ApiKeyId:   keyID,
		Name:       "new-name",
		Permission: "full",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "new-name", view["name"])
	require.Equal(t, "full_access", view["permission"], "permission should be normalized")

	var key repo.APIKey
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&key, keyID).Error)
	require.Equal(t, "new-name", key.Name)
	require.Equal(t, "full_access", key.Permission)
	// 权限变更后 resourceKeys 应重算为 full 集合。
	for _, rk := range repo.APIKeyResourceKeysForPermission("full") {
		require.Contains(t, key.ResourceKeys, rk)
	}
}

func TestApiKeyUpdate_NameOnly(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	keyID, _ := mustCreateAPIKey(t, svcCtx, ctx, "orig")

	l := NewApiKeyUpdateLogic(ctx, svcCtx)
	resp, err := l.ApiKeyUpdate(&types.ApiKeyUpdateReq{ApiKeyId: keyID, Name: "renamed"})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "renamed", view["name"])
	require.Equal(t, "read_only", view["permission"], "permission should be unchanged")
}

func TestApiKeyUpdate_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewApiKeyUpdateLogic(ctx, svcCtx)

	_, err := l.ApiKeyUpdate(&types.ApiKeyUpdateReq{ApiKeyId: 999999, Name: "ghost"})
	requireHTTPError(t, err, 404)
}

func TestApiKeyUpdate_TooLongNameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	keyID, _ := mustCreateAPIKey(t, svcCtx, ctx, "orig")

	l := NewApiKeyUpdateLogic(ctx, svcCtx)
	_, err := l.ApiKeyUpdate(&types.ApiKeyUpdateReq{ApiKeyId: keyID, Name: strings.Repeat("a", 101)})
	requireHTTPError(t, err, 400)
}
