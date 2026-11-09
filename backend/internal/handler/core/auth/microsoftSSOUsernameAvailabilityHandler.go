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

func MicrosoftSSOUsernameAvailabilityHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var req types.ExternalSSOUsernameAvailabilityReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := auth.NewMicrosoftSSOUsernameAvailabilityLogic(r.Context(), svcCtx)
		resp, err := l.MicrosoftSSOUsernameAvailability(&req, microsoftBrowserBinding(r))
		if err != nil {
			respx.WriteError(w, err)
		} else {
			gozerohttpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
