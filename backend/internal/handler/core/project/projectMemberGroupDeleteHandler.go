// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package project

import (
	"net/http"

	"backend/internal/logic/core/project"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ProjectMemberGroupDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ProjectMemberGroupReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := project.NewProjectMemberGroupDeleteLogic(r.Context(), svcCtx)
		resp, err := l.ProjectMemberGroupDelete(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
