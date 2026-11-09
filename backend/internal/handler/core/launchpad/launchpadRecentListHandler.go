// Code scaffolded by goctl. Safe to edit.

package launchpad

import (
	respx "backend/internal/httpx"
	"backend/internal/logic/core/launchpad"
	"backend/internal/svc"
	"net/http"

	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

func LaunchpadRecentListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := launchpad.NewLaunchpadRecentListLogic(r.Context(), svcCtx)
		resp, err := l.LaunchpadRecentList()
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		gozerohttpx.OkJson(w, resp)
	}
}
