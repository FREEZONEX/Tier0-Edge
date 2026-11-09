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

func NotificationListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := parse[types.NotificationListReq](w, r)
		if !ok {
			return
		}
		resp, err := logicnotify.NewLogic(r.Context(), svcCtx).NotificationList(req)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
