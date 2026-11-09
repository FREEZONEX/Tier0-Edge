// TASK-028 OAuthClientCreateLogic 单元测试：
//
//	目标：覆盖创建 OAuth 客户端入口。
//	正常路径：创建成功返回 client（含生成的 client_id、默认 TTL 3600）；
//	          redirectUris 空项被跳过并序列化为 JSON；created_by 记录操作人。
//	异常情况：空 clientName → 400；名称超长 → 400；redirectUri 超长 → 400。
package oauth

import (
	"encoding/json"
	"testing"

	"backend/internal/domain/common"
	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOAuthClientCreate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientCreateLogic(ctx, svcCtx)

	resp, err := l.OAuthClientCreate(&types.OAuthClientReq{
		ClientName:     "my-app",
		RedirectUris:   []string{"http://app.example.com/callback", "  "},
		AllowedOrigins: []string{"http://app.example.com"},
		AppId:          7,
	})
	require.NoError(t, err)
	view := respData(t, resp)
	client, ok := view["client"].(repo.OAuthClient)
	require.True(t, ok, "client should be repo.OAuthClient")
	require.NotEmpty(t, client.ClientID)
	require.Equal(t, "my-app", client.ClientName)
	require.Equal(t, int64(7), client.AppID)
	require.Equal(t, 3600, client.AccessTokenTTLSeconds)
	require.True(t, client.Enabled)
	require.Equal(t, int64(1), client.CreatedBy)

	// 空项被跳过
	var redirects []string
	require.NoError(t, json.Unmarshal(client.RedirectURIs, &redirects))
	require.Equal(t, []string{"http://app.example.com/callback"}, redirects)
}

func TestOAuthClientCreate_EmptyName(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientCreateLogic(ctx, svcCtx)

	_, err := l.OAuthClientCreate(&types.OAuthClientReq{ClientName: "  "})
	requireHTTPError(t, err, 400)
}

func TestOAuthClientCreate_NameTooLong(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientCreateLogic(ctx, svcCtx)

	_, err := l.OAuthClientCreate(&types.OAuthClientReq{
		ClientName: repeatRune('n', common.ConnectionNameMaxLen+1),
	})
	requireHTTPError(t, err, 400)
}

func TestOAuthClientCreate_RedirectTooLong(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientCreateLogic(ctx, svcCtx)

	_, err := l.OAuthClientCreate(&types.OAuthClientReq{
		ClientName:   "ok-name",
		RedirectUris: []string{"http://x/" + repeatRune('r', 513)},
	})
	requireHTTPError(t, err, 400)
}
