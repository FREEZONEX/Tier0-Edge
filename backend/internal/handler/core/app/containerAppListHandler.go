// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package app

import (
	"net/http"

	"backend/internal/logic/core/app"
	"backend/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ContainerAppListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := app.NewContainerAppListLogic(r.Context(), svcCtx)
		resp, err := l.ContainerAppList()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
