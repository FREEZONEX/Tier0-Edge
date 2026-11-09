// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package gateway

import (
	respx "backend/internal/httpx"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"

	"backend/internal/logic/core/gateway"
	"backend/internal/svc"
)

func GatewayRouteListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := gateway.NewGatewayRouteListLogic(r.Context(), svcCtx)
		resp, err := l.GatewayRouteList()
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
