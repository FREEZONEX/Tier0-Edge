// Thin HTTP adapter maintained alongside the notification API contract.
package notifications

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/notifications"
	"backend/internal/svc"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"net/http"
)

func DetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := notifications.NewLogic(r.Context(), svcCtx)
		resp, err := l.Detail(pathvar.Vars(r)["id"])
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, respx.Envelope(resp))
	}
}
