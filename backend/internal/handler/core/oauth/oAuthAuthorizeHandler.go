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
)

func OAuthAuthorizeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OAuthAuthorizeReq
		standard := hasStandardAuthorizeParams(r)
		if standard {
			fillStandardAuthorizeParams(r, &req)
		} else {
			if err := gozerohttpx.Parse(r, &req); err != nil {
				respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
				return
			}
		}

		l := oauth.NewOAuthAuthorizeLogic(r.Context(), svcCtx)
		resp, err := l.OAuthAuthorize(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		if standard {
			if data, ok := resp.Data.(map[string]any); ok {
				if redirectURI, ok := data["redirectUri"].(string); ok && redirectURI != "" {
					http.Redirect(w, r, redirectURI, http.StatusFound)
					return
				}
			}
		}
		gozerohttpx.OkJson(w, resp)
	}
}

func hasStandardAuthorizeParams(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("response_type") != "" || q.Get("client_id") != "" || q.Get("redirect_uri") != ""
}

func fillStandardAuthorizeParams(r *http.Request, req *types.OAuthAuthorizeReq) {
	q := r.URL.Query()
	if v := q.Get("response_type"); v != "" {
		req.ResponseType = v
	}
	if v := q.Get("client_id"); v != "" {
		req.ClientId = v
	}
	if v := q.Get("redirect_uri"); v != "" {
		req.RedirectUri = v
	}
	if v := q.Get("code_challenge"); v != "" {
		req.CodeChallenge = v
	}
	if v := q.Get("code_challenge_method"); v != "" {
		req.CodeChallengeMethod = v
	}
	req.Scope = q.Get("scope")
	req.State = q.Get("state")
}
