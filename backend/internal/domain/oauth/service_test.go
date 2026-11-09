package oauth

// 测试目标：OAuth 服务（service.go）。
// 覆盖的正常路径：纯函数 redirectAllowed / normalizeRedirectURIs / normalizeNotFound；
// 客户端 CRUD 与 Authorize/Token/UserInfo 的 DB 路径（testkit）。
// 覆盖的异常情况：非法响应类型、非法 redirect、超长 redirect URI、code 不匹配、
// 不存在的客户端/用户。
// 说明：svctest 依赖 domain 包（import cycle），oauth 测试使用 testkit 等价装配。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestOAuth(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	return New(ctx), ctx
}

func TestRedirectAllowed(t *testing.T) {
	raw := json.RawMessage(`["https://app.example.com/cb","http://localhost:3000/cb"]`)
	if !redirectAllowed(raw, "https://app.example.com/cb") {
		t.Fatal("exact match should be allowed")
	}
	if redirectAllowed(raw, "https://evil.example.com/cb") {
		t.Fatal("non-listed redirect should be denied")
	}
	if redirectAllowed(raw, "") {
		t.Fatal("empty redirect should be denied")
	}
	if redirectAllowed(json.RawMessage(`not-json`), "https://app.example.com/cb") {
		t.Fatal("malformed json should be denied")
	}
}

func TestNormalizeRedirectURIs(t *testing.T) {
	out, err := normalizeRedirectURIs([]string{"  https://a.com/cb  ", "", "http://b.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"https://a.com/cb", "http://b.com"}, out)

	long := "https://a.com/" + string(make([]byte, 600))
	_, err = normalizeRedirectURIs([]string{long})
	require.ErrorIs(t, err, ErrInvalidRedirect)
}

func TestNormalizeNotFound(t *testing.T) {
	require.ErrorIs(t, normalizeNotFound(repo.ErrNotFound), ErrNotFound)
	plain := errors.New("boom")
	require.ErrorIs(t, normalizeNotFound(plain), plain)
}

func TestOAuthClientCRUD(t *testing.T) {
	svc, ctx := newTestOAuth(t)

	// 创建客户端
	created, err := svc.CreateClient(ctx, ClientCommand{
		ClientName:   "My App",
		RedirectURIs: []string{"https://app.example.com/cb"},
		AppID:        1,
		UserID:       1,
	})
	require.NoError(t, err)
	client, ok := created["client"].(repo.OAuthClient)
	require.True(t, ok)
	require.True(t, client.ID > 0)
	require.Equal(t, "My App", client.ClientName)

	// 非法名称
	_, err = svc.CreateClient(ctx, ClientCommand{ClientName: ""})
	require.ErrorIs(t, err, ErrInvalid)

	// 列表
	list, err := svc.ListClients(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, list["total"])

	// 更新
	updated, err := svc.UpdateClient(ctx, ClientCommand{
		ID:           client.ID,
		ClientName:   "My App 2",
		RedirectURIs: []string{"https://app.example.com/cb2"},
		UserID:       1,
	})
	require.NoError(t, err)
	updClient, ok := updated["client"].(repo.OAuthClient)
	require.True(t, ok)
	require.Equal(t, "My App 2", updClient.ClientName)

	// 更新不存在
	_, err = svc.UpdateClient(ctx, ClientCommand{ID: 999999, ClientName: "x", UserID: 1})
	require.ErrorIs(t, err, ErrNotFound)

	// 删除
	deleted, err := svc.DeleteClient(ctx, client.ID)
	require.NoError(t, err)
	require.Equal(t, true, deleted["deleted"])
	// 重复删除幂等成功（生产行为：软删 Update enabled=false 不校验已有状态）
	_, err = svc.DeleteClient(ctx, client.ID)
	require.NoError(t, err)
	// 删除后客户端不可见
	_, err = svc.Authorize(ctx, AuthorizeCommand{ResponseType: "code", ClientID: client.ClientID, RedirectURI: "https://app.example.com/cb", UserID: 1})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestOAuthAuthorizeAndToken(t *testing.T) {
	svc, ctx := newTestOAuth(t)
	redirect := "https://app.example.com/cb"

	created, err := svc.CreateClient(ctx, ClientCommand{
		ClientName:   "auth-app",
		RedirectURIs: []string{redirect},
		UserID:       1,
	})
	require.NoError(t, err)
	client := created["client"].(repo.OAuthClient)

	// 非法响应类型
	_, err = svc.Authorize(ctx, AuthorizeCommand{ResponseType: "token", ClientID: client.ClientID, RedirectURI: redirect, UserID: 1})
	require.ErrorIs(t, err, ErrInvalid)
	// 无用户
	_, err = svc.Authorize(ctx, AuthorizeCommand{ResponseType: "code", ClientID: client.ClientID, RedirectURI: redirect})
	require.ErrorIs(t, err, ErrInvalid)

	// 客户端不存在
	_, err = svc.Authorize(ctx, AuthorizeCommand{ResponseType: "code", ClientID: "nope", RedirectURI: redirect, UserID: 1})
	require.ErrorIs(t, err, ErrNotFound)

	// redirect 不匹配
	_, err = svc.Authorize(ctx, AuthorizeCommand{ResponseType: "code", ClientID: client.ClientID, RedirectURI: "https://evil.com", UserID: 1})
	require.ErrorIs(t, err, ErrInvalidRedirect)

	// 正常授权
	resp, err := svc.Authorize(ctx, AuthorizeCommand{
		ResponseType: "code",
		ClientID:     client.ClientID,
		RedirectURI:  redirect,
		Scope:        "openid",
		State:        "xyz",
		UserID:       1,
	})
	require.NoError(t, err)
	code, ok := resp["code"].(string)
	require.True(t, ok && code != "")
	require.Equal(t, "xyz", resp["state"])
	require.Contains(t, resp["redirectUri"].(string), "code=")

	// Token：非法 grant type
	_, err = svc.Token(ctx, TokenCommand{GrantType: "password"})
	require.ErrorIs(t, err, ErrInvalid)

	// Token：客户端不存在
	_, err = svc.Token(ctx, TokenCommand{GrantType: "authorization_code", ClientID: "nope", Code: code, RedirectURI: redirect})
	require.ErrorIs(t, err, ErrNotFound)

	// Token：code 不匹配
	_, err = svc.Token(ctx, TokenCommand{GrantType: "authorization_code", ClientID: client.ClientID, Code: "wrong", RedirectURI: redirect})
	require.ErrorIs(t, err, ErrInvalidCode)

	// Token：正常兑换
	tokenResp, err := svc.Token(ctx, TokenCommand{
		GrantType:   "authorization_code",
		ClientID:    client.ClientID,
		Code:        code,
		RedirectURI: redirect,
	})
	require.NoError(t, err)
	require.NotEmpty(t, tokenResp["accessToken"])
	require.Equal(t, "Bearer", tokenResp["tokenType"])
	require.Equal(t, "openid", tokenResp["scope"])
	require.Equal(t, 3600, tokenResp["expiresIn"])

	// code 只能消费一次
	_, err = svc.Token(ctx, TokenCommand{GrantType: "authorization_code", ClientID: client.ClientID, Code: code, RedirectURI: redirect})
	require.ErrorIs(t, err, ErrInvalidCode)
}

func TestOAuthUserInfo(t *testing.T) {
	svc, ctx := newTestOAuth(t)

	info, err := svc.UserInfo(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), info["userId"])
	// sub 回填为 userId
	require.Equal(t, info["userId"], info["sub"])

	_, err = svc.UserInfo(ctx, 999999)
	require.ErrorIs(t, err, ErrNotFound)
}
