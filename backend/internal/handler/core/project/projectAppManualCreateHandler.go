// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package project

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/core/project"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func ProjectAppManualCreateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ProjectAppManualSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}
		resp, err := project.NewProjectAppManualCreateLogic(r.Context(), svcCtx).ProjectAppManualCreate(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
