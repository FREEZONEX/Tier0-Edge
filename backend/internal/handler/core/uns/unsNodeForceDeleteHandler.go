// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package uns

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/uns"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
	"net/http"
)

func UnsNodeForceDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UnsForceDeleteReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := uns.NewUnsNodeForceDeleteLogic(r.Context(), svcCtx)
		resp, err := l.UnsNodeForceDelete(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
