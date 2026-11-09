// Thin HTTP adapter maintained alongside the notification API contract.
package notifications

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/notifications"
	"backend/internal/svc"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func PageHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req notifications.PageQuery
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		// sourceIds 可能以 sourceIds[]= 形式传入（axios 默认数组序列化），两种键名都收
		if len(req.SourceIds) == 0 {
			req.SourceIds = r.URL.Query()["sourceIds[]"]
		}
		l := notifications.NewLogic(r.Context(), svcCtx)
		resp, err := l.Page(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(resp))
	}
}
