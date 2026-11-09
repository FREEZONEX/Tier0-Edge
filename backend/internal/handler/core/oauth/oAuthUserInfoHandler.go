// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package oauth

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/oauth"
	"backend/internal/svc"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
	"strings"
)

func OAuthUserInfoHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := oauth.NewOAuthUserInfoLogic(r.Context(), svcCtx)
		resp, err := l.OAuthUserInfo()
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		if wantsStandardUserInfo(r) {
			gozerohttpx.OkJson(w, standardUserInfoResponse(resp.Data))
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}

func wantsStandardUserInfo(r *http.Request) bool {
	if r.Header.Get("X-Sa-Token") != "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Authorization")), "bearer ")
}

func standardUserInfoResponse(data any) map[string]any {
	src, _ := data.(map[string]any)
	userName, _ := src["userName"].(string)
	nickName, _ := src["nickName"].(string)
	email, _ := src["email"].(string)
	out := map[string]any{
		"sub":                src["sub"],
		"user_id":            src["userId"],
		"user_name":          userName,
		"preferred_username": userName,
		"name":               nickName,
		"email":              email,
		"roleList":           src["roleList"],
	}
	if out["name"] == "" {
		out["name"] = userName
	}
	return out
}
