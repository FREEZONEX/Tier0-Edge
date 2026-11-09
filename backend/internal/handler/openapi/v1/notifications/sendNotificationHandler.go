// Thin HTTP adapter maintained alongside the notification API contract.
package notifications

import (
	"backend/internal/domain/notify/openapiresp"
	"backend/internal/logic/openapi/v1/notifications"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func SendNotificationHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SendNotificationReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			openapiresp.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request: "+err.Error())
			return
		}
		l := notifications.NewSendNotificationLogic(r.Context(), svcCtx)
		resp, err := l.SendNotification(&req)
		if err != nil {
			openapiresp.WriteLogicError(w, err)
			return
		}
		openapiresp.WriteOK(w, resp)
	}
}
