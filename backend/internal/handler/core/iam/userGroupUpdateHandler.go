// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package iam

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/core/iam"
	"backend/internal/svc"
	"backend/internal/types"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func UserGroupUpdateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UserGroupSaveReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := iam.NewUserGroupUpdateLogic(r.Context(), svcCtx)
		resp, err := l.UserGroupUpdate(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
