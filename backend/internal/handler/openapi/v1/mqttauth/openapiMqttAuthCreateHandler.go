// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package mqttauth

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/openapi/v1/mqttauth"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

// 创建 MQTT Auth
func OpenapiMqttAuthCreateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OpenapiMqttAuthCreateReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := mqttauth.NewOpenapiMqttAuthCreateLogic(r.Context(), svcCtx)
		resp, err := l.OpenapiMqttAuthCreate(&req)
		if err != nil {
			respx.WriteError(w, err)
		} else {
			gozerohttpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
