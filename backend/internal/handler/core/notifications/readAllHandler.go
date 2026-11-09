// Thin HTTP adapter maintained alongside the notification API contract.
package notifications

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/notifications"
	"backend/internal/svc"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func ReadAllHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req notifications.ReadAllFilter
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		l := notifications.NewLogic(r.Context(), svcCtx)
		affected, err := l.ReadAll(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(map[string]any{"affected": affected, "count": affected}))
	}
}
