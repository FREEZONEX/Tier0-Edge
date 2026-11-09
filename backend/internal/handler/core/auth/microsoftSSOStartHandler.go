// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package auth

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/core/auth"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func MicrosoftSSOStartHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ExternalSSOStartReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := auth.NewMicrosoftSSOStartLogic(r.Context(), svcCtx)
		result, err := l.MicrosoftSSOStart(&req)
		if err != nil {
			redirectMicrosoftError(w, r, err)
			return
		}
		setMicrosoftStateCookie(w, svcCtx, result.BrowserBinding)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, result.AuthorizationURL, http.StatusFound)
	}
}
