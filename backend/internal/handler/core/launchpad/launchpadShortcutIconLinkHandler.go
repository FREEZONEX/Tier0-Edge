// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package launchpad

import (
	"net/http"

	"backend/internal/logic/core/launchpad"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func LaunchpadShortcutIconLinkHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LaunchpadAppViewReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := launchpad.NewLaunchpadShortcutIconLinkLogic(r.Context(), svcCtx)
		resp, err := l.LaunchpadShortcutIconLink(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
