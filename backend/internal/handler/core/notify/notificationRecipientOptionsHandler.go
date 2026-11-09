// Thin HTTP adapter maintained alongside the notification API contract.
package notify

import (
	"net/http"

	"backend/internal/httpx"
	logicnotify "backend/internal/logic/core/notify"
	"backend/internal/svc"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func NotificationRecipientOptionsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := logicnotify.NewLogic(r.Context(), svcCtx).NotificationRecipientOptions()
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
