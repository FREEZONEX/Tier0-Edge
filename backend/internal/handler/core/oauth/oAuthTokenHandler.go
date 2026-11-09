// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package oauth

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/oauth"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
	"strings"
)

func OAuthTokenHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OAuthTokenReq
		standard := hasStandardTokenParams(r)
		if standard {
			fillStandardTokenParams(r, &req)
		} else {
			if err := gozerohttpx.Parse(r, &req); err != nil {
				respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
				return
			}
		}

		l := oauth.NewOAuthTokenLogic(r.Context(), svcCtx)
		resp, err := l.OAuthToken(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		if standard {
			gozerohttpx.OkJson(w, standardTokenResponse(resp.Data))
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}

func hasStandardTokenParams(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(contentType, "application/x-www-form-urlencoded")
}

func fillStandardTokenParams(r *http.Request, req *types.OAuthTokenReq) {
	if err := r.ParseForm(); err != nil {
		return
	}
	if v := r.Form.Get("grant_type"); v != "" {
		req.GrantType = v
	}
	if v := r.Form.Get("redirect_uri"); v != "" {
		req.RedirectUri = v
	}
	if v := r.Form.Get("client_id"); v != "" {
		req.ClientId = v
	}
	if v := r.Form.Get("code_verifier"); v != "" {
		req.CodeVerifier = v
	}
	if v := r.Form.Get("code"); v != "" {
		req.Code = v
	}
	if req.GrantType == "" {
		req.GrantType = r.Form.Get("grantType")
	}
	if req.RedirectUri == "" {
		req.RedirectUri = r.Form.Get("redirectUri")
	}
	if req.ClientId == "" {
		req.ClientId = r.Form.Get("clientId")
	}
	if req.CodeVerifier == "" {
		req.CodeVerifier = r.Form.Get("codeVerifier")
	}
}

func standardTokenResponse(data any) map[string]any {
	src, _ := data.(map[string]any)
	out := map[string]any{}
	if v, ok := src["accessToken"]; ok {
		out["access_token"] = v
	}
	if v, ok := src["tokenType"]; ok {
		out["token_type"] = v
	}
	if v, ok := src["expiresIn"]; ok {
		out["expires_in"] = v
	}
	if v, ok := src["scope"]; ok {
		out["scope"] = v
	}
	return out
}
