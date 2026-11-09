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

func MicrosoftSSOCallbackHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ExternalSSOCallbackReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := auth.NewMicrosoftSSOCallbackLogic(r.Context(), svcCtx)
		result, err := l.MicrosoftSSOCallback(&req, microsoftStateBinding(r))
		clearMicrosoftStateCookie(w, svcCtx)
		if err != nil {
			redirectMicrosoftError(w, r, err)
			return
		}
		redirectMicrosoftCallback(w, r, svcCtx, result)
	}
}
