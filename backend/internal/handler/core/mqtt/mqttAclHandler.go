// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package mqtt

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/mqtt"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func MqttAclHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.MqttAclReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := mqtt.NewMqttAclLogic(r.Context(), svcCtx)
		resp, err := l.MqttAcl(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		writeMqttDecision(w, resp.Data)
	}
}
