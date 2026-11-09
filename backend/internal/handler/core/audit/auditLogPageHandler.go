// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package audit

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/audit"
	"backend/internal/svc"
	"backend/internal/types"
	"net/http"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func AuditLogPageHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AuditLogPageReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := audit.NewAuditLogPageLogic(r.Context(), svcCtx)
		resp, err := l.AuditLogPage(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
