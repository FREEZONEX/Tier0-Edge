// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package notify

import (
	"net/http"

	"backend/internal/handler/handlerx"
	respx "backend/internal/httpx"
	"backend/internal/logic/core/notify"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func UserMessageSenderIconHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.UserMessageSenderIconReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := notify.NewLogic(r.Context(), svcCtx)
		assetID, err := l.UserMessageSenderIcon(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		// 与 Launchpad 取图一致 inline 返回，图标才能直接进 <img>；
		// StreamAssetByID 仍会把 HTML/SVG 等可执行内容强制成 attachment
		handlerx.StreamAssetByID(w, r, svcCtx, assetID, false)
	}
}
