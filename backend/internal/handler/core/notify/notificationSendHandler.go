// Thin HTTP adapter maintained alongside the notification API contract.
package notify

import (
	"backend/internal/httpx"
	logicnotify "backend/internal/logic/core/notify"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func NotificationSendHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := parse[types.NotificationSendReq](w, r)
		if !ok {
			return
		}
		resp, err := logicnotify.NewLogic(r.Context(), svcCtx).NotificationSend(req)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
