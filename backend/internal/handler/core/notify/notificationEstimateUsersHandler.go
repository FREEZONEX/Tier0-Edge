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

func NotificationEstimateUsersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := parse[types.NotificationEstimateUsersReq](w, r)
		if !ok {
			return
		}
		resp, err := logicnotify.NewLogic(r.Context(), svcCtx).NotificationEstimateUsers(req)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
