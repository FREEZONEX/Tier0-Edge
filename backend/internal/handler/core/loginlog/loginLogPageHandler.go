// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package loginlog

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/core/loginlog"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func LoginLogPageHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LoginLogPageReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := loginlog.NewLoginLogPageLogic(r.Context(), svcCtx)
		resp, err := l.LoginLogPage(&req)
		if err != nil {
			respx.WriteError(w, err)
		} else {
			gozerohttpx.OkJson(w, resp)
		}
	}
}
