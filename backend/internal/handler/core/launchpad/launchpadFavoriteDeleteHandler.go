// Code scaffolded by goctl. Safe to edit.

package launchpad

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/launchpad"
	"backend/internal/svc"
	"backend/internal/types"
	"net/http"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func LaunchpadFavoriteDeleteHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LaunchpadFavoriteBatchReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := launchpad.NewLaunchpadFavoriteDeleteLogic(r.Context(), svcCtx)
		resp, err := l.LaunchpadFavoriteDelete(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
