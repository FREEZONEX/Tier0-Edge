// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"net/http"

	respx "backend/internal/httpx"
	systemlogic "backend/internal/logic/core/system"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func LoginSessionDurationPutHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SystemLoginSessionDurationReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, err)
			return
		}
		l := systemlogic.NewLoginSessionDurationPutLogic(r.Context(), svcCtx)
		resp, err := l.LoginSessionDurationPut(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
