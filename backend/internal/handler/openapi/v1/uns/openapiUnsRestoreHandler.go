// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package uns

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/openapi/v1/uns"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func OpenapiUnsRestoreHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.NodeRestoreReq
		if err := httpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := uns.NewOpenapiUnsRestoreLogic(r.Context(), svcCtx)
		resp, err := l.OpenapiUnsRestore(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		httpx.OkJson(w, resp)
	}
}
