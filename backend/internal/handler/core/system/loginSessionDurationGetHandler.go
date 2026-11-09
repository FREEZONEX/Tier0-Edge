// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"net/http"

	respx "backend/internal/httpx"
	systemlogic "backend/internal/logic/core/system"
	"backend/internal/svc"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func LoginSessionDurationGetHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := systemlogic.NewLoginSessionDurationGetLogic(r.Context(), svcCtx)
		resp, err := l.LoginSessionDurationGet()
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
