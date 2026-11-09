// Thin HTTP adapter maintained alongside the notification API contract.
package notifications

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/notifications"
	"backend/internal/svc"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func UnreadCountHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := notifications.NewLogic(r.Context(), svcCtx)
		count, err := l.UnreadCount()
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(map[string]any{"count": count}))
	}
}
