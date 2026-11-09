// Thin HTTP adapter maintained alongside the notification API contract.
package notify

import (
	"backend/internal/httpx"
	logicnotify "backend/internal/logic/core/notify"
	"backend/internal/svc"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func UserMessageUnreadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logicnotify.NewLogic(r.Context(), svcCtx)
		resp, err := l.UserMessageUnread()
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
