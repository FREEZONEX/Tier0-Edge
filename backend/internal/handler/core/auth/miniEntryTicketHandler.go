package auth

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/auth"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
	"strings"
)

func MiniEntryTicketHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// Native handoff must be deliberately requested with an Authorization header.
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			respx.WriteError(w, respx.NewHTTPError(401, "unauthorized"))
			return
		}
		var req types.MiniEntryTicketReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(400, "invalid request"))
			return
		}
		resp, err := auth.NewMiniEntryLogic(r.Context(), svcCtx).Ticket(&req, strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
