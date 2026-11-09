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

func LaunchpadAppIconHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LaunchpadAppViewReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := launchpad.NewLaunchpadAppIconLogic(r.Context(), svcCtx)
		assetID, err := l.LaunchpadAppIcon(&req)
		if err != nil {
			respx.WriteError(w, err)
			return
		}
		// 图片要能直接进 <img>，所以 inline 返回；StreamAssetByID 内部仍会把 HTML/SVG 等
		// 可执行内容强制成 attachment，避免内联上传物在本域执行脚本
		handlerx.StreamAssetByID(w, r, svcCtx, assetID, false)
	}
}
