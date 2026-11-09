// TASK-028 OAuthAuthorizeLogic 单元测试：
//
//	目标：覆盖授权码签发入口（authorization code flow）。
//	正常路径：合法 responseType=code + 已注册 redirectUri → 返回 code，
//	          且 redirectUri 回填 code/state 查询参数；state 透传。
//	异常情况：非 code 类型 → 400；未登录（UserID=0）→ 400；
//	          未知 client_id → 404；redirectUri 不在白名单 → 400。
package oauth

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOAuthAuthorize_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "auth-app", "http://app.example.com/callback")

	l := NewOAuthAuthorizeLogic(ctx, svcCtx)
	resp, err := l.OAuthAuthorize(&types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://app.example.com/callback",
		Scope:        "openid",
		State:        "xyz",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.NotEmpty(t, view["code"])
	require.Equal(t, "xyz", view["state"])

	// redirectUri 应携带 code 与 state
	redirect, ok := view["redirectUri"].(string)
	require.True(t, ok)
	u, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Equal(t, "http://app.example.com/callback", strings.TrimSuffix(redirect, "?"+u.RawQuery))
	require.NotEmpty(t, u.Query().Get("code"))
	require.Equal(t, "xyz", u.Query().Get("state"))
}

func TestOAuthAuthorize_NoState(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "auth-app2", "http://app2.example.com/callback")

	l := NewOAuthAuthorizeLogic(ctx, svcCtx)
	resp, err := l.OAuthAuthorize(&types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://app2.example.com/callback",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Empty(t, view["state"])
	u, err := url.Parse(view["redirectUri"].(string))
	require.NoError(t, err)
	require.Empty(t, u.Query().Get("state"))
	require.NotEmpty(t, u.Query().Get("code"))
}

func TestOAuthAuthorize_InvalidResponseType(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "auth-app3", "http://app3.example.com/callback")

	l := NewOAuthAuthorizeLogic(ctx, svcCtx)
	_, err := l.OAuthAuthorize(&types.OAuthAuthorizeReq{
		ResponseType: "token",
		ClientId:     clientID,
		RedirectUri:  "http://app3.example.com/callback",
	})
	requireHTTPError(t, err, 400)
}

func TestOAuthAuthorize_NoUser(t *testing.T) {
	svcCtx := newSvcEnvNoUser(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, context.Background(), "auth-app4", "http://app4.example.com/callback")

	l := NewOAuthAuthorizeLogic(context.Background(), svcCtx)
	_, err := l.OAuthAuthorize(&types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://app4.example.com/callback",
	})
	requireHTTPError(t, err, 400)
}

func TestOAuthAuthorize_UnknownClient(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthAuthorizeLogic(ctx, svcCtx)

	_, err := l.OAuthAuthorize(&types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     "oc_missing",
		RedirectUri:  "http://app.example.com/callback",
	})
	requireHTTPError(t, err, 404)
}

func TestOAuthAuthorize_RedirectNotAllowed(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "auth-app5", "http://app5.example.com/callback")

	l := NewOAuthAuthorizeLogic(ctx, svcCtx)
	_, err := l.OAuthAuthorize(&types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://evil.example.com/callback",
	})
	requireHTTPError(t, err, 400)
}
