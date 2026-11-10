package fleet

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func FleetCurrentConnectionUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.FleetCurrentConnectionReq
		if err := gozerohttpx.ParseJsonBody(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request"))
			return
		}
		status, err := svcCtx.App.Cluster.ConfigureCurrentConnection(r.Context(), req.AccessToken)
		if err != nil {
			respx.WriteError(w, logicx.Error(err))
			return
		}
		gozerohttpx.OkJsonCtx(r.Context(), w, respx.Envelope(status))
	}
}
