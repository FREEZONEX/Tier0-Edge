// TASK-028 OAuthClientUpdateLogic 单元测试：
//
//	目标：覆盖更新 OAuth 客户端入口。
//	正常路径：更新名称/回调/来源域成功，updated_by 记录操作人；
//	          redirectUris 空项被跳过。
//	异常情况：空 clientName → 400；不存在的 id → 404。
package oauth

import (
	"encoding/json"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOAuthClientUpdate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	id, _ := mustCreateOAuthClient(t, svcCtx, ctx, "before", "http://old.example.com/cb")

	l := NewOAuthClientUpdateLogic(ctx, svcCtx)
	resp, err := l.OAuthClientUpdate(&types.OAuthClientReq{
		Id:             id,
		ClientName:     "after",
		RedirectUris:   []string{"http://new.example.com/cb", ""},
		AllowedOrigins: []string{"http://new.example.com"},
	})
	require.NoError(t, err)
	view := respData(t, resp)
	client, ok := view["client"].(repo.OAuthClient)
	require.True(t, ok, "client should be repo.OAuthClient")
	require.Equal(t, "after", client.ClientName)
	require.Equal(t, int64(1), client.UpdatedBy)

	var redirects []string
	require.NoError(t, json.Unmarshal(client.RedirectURIs, &redirects))
	require.Equal(t, []string{"http://new.example.com/cb"}, redirects)
}

func TestOAuthClientUpdate_EmptyName(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientUpdateLogic(ctx, svcCtx)

	_, err := l.OAuthClientUpdate(&types.OAuthClientReq{Id: 1, ClientName: " "})
	requireHTTPError(t, err, 400)
}

func TestOAuthClientUpdate_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientUpdateLogic(ctx, svcCtx)

	_, err := l.OAuthClientUpdate(&types.OAuthClientReq{
		Id:           999999,
		ClientName:   "nope",
		RedirectUris: []string{"http://x.example.com/cb"},
	})
	requireHTTPError(t, err, 404)
}
