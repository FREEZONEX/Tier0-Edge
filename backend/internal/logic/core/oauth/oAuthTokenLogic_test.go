// TASK-028 OAuthTokenLogic 单元测试：
//
//	目标：覆盖授权码换 access token 入口。
//	正常路径：authorization_code + 未使用的 code → 返回 accessToken/tokenType/expiresIn/scope；
//	          code 只能消费一次（重复使用 → 400）。
//	异常情况：非法 grant_type → 400；未知 client_id → 404；
//	          code 不存在 → 400；code 的 client/redirect 不匹配 → 400。
package oauth

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// mustAuthorize 走一遍完整 Authorize 流程拿到可用的授权码。
func mustAuthorize(t *testing.T, l *OAuthAuthorizeLogic, req types.OAuthAuthorizeReq) string {
	t.Helper()
	resp, err := l.OAuthAuthorize(&req)
	require.NoError(t, err)
	code, ok := respData(t, resp)["code"].(string)
	require.True(t, ok)
	require.NotEmpty(t, code)
	return code
}

func TestOAuthToken_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "token-app", "http://tok.example.com/cb")

	auth := NewOAuthAuthorizeLogic(ctx, svcCtx)
	code := mustAuthorize(t, auth, types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://tok.example.com/cb",
		Scope:        "openid",
	})

	l := NewOAuthTokenLogic(ctx, svcCtx)
	resp, err := l.OAuthToken(&types.OAuthTokenReq{
		GrantType:   "authorization_code",
		Code:        code,
		RedirectUri: "http://tok.example.com/cb",
		ClientId:    clientID,
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.NotEmpty(t, view["accessToken"])
	require.Equal(t, "Bearer", view["tokenType"])
	require.Equal(t, 3600, view["expiresIn"])
	require.Equal(t, "openid", view["scope"])
}

func TestOAuthToken_CodeSingleUse(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "token-app2", "http://tok2.example.com/cb")

	auth := NewOAuthAuthorizeLogic(ctx, svcCtx)
	code := mustAuthorize(t, auth, types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://tok2.example.com/cb",
	})

	l := NewOAuthTokenLogic(ctx, svcCtx)
	req := &types.OAuthTokenReq{
		GrantType:   "authorization_code",
		Code:        code,
		RedirectUri: "http://tok2.example.com/cb",
		ClientId:    clientID,
	}
	_, err := l.OAuthToken(req)
	require.NoError(t, err)
	_, err = l.OAuthToken(req)
	requireHTTPError(t, err, 400)
}

func TestOAuthToken_InvalidGrantType(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthTokenLogic(ctx, svcCtx)

	_, err := l.OAuthToken(&types.OAuthTokenReq{
		GrantType: "client_credentials",
		ClientId:  "oc_x",
	})
	requireHTTPError(t, err, 400)
}

func TestOAuthToken_UnknownClient(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthTokenLogic(ctx, svcCtx)

	_, err := l.OAuthToken(&types.OAuthTokenReq{
		GrantType: "authorization_code",
		Code:      "ocd_whatever",
		ClientId:  "oc_missing",
	})
	requireHTTPError(t, err, 404)
}

func TestOAuthToken_InvalidCode(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "token-app3", "http://tok3.example.com/cb")

	l := NewOAuthTokenLogic(ctx, svcCtx)
	_, err := l.OAuthToken(&types.OAuthTokenReq{
		GrantType:   "authorization_code",
		Code:        "ocd_not-a-real-code",
		RedirectUri: "http://tok3.example.com/cb",
		ClientId:    clientID,
	})
	requireHTTPError(t, err, 400)
}

func TestOAuthToken_CodeMismatchedClient(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	_, clientID := mustCreateOAuthClient(t, svcCtx, ctx, "token-app4", "http://tok4.example.com/cb")
	_, otherID := mustCreateOAuthClient(t, svcCtx, ctx, "token-app5", "http://tok5.example.com/cb")

	auth := NewOAuthAuthorizeLogic(ctx, svcCtx)
	code := mustAuthorize(t, auth, types.OAuthAuthorizeReq{
		ResponseType: "code",
		ClientId:     clientID,
		RedirectUri:  "http://tok4.example.com/cb",
	})

	l := NewOAuthTokenLogic(ctx, svcCtx)
	_, err := l.OAuthToken(&types.OAuthTokenReq{
		GrantType:   "authorization_code",
		Code:        code,
		RedirectUri: "http://tok4.example.com/cb",
		ClientId:    otherID,
	})
	requireHTTPError(t, err, 400)
}
