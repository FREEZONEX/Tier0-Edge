// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package launchpad

import (
	"net/http"

	"backend/internal/handler/handlerx"
	respx "backend/internal/httpx"
	"backend/internal/logic/core/launchpad"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func LaunchpadShortcutIconHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LaunchpadShortcutIconReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := launchpad.NewLaunchpadShortcutIconLogic(r.Context(), svcCtx)
		assetID, err := l.LaunchpadShortcutIcon(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		// 与 /apps/{id}/icon 一致 inline 返回，图标才能直接进 <link rel="apple-touch-icon">；
		// StreamAssetByID 仍会把 HTML/SVG 等可执行内容强制成 attachment
		handlerx.StreamAssetByID(w, r, svcCtx, assetID, false)
	}
}
