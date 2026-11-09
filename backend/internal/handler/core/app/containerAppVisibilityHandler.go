// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package app

import (
	"net/http"

	"backend/internal/logic/core/app"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ContainerAppVisibilityHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ContainerAppVisibilityReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := app.NewContainerAppVisibilityLogic(r.Context(), svcCtx)
		resp, err := l.ContainerAppVisibility(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
