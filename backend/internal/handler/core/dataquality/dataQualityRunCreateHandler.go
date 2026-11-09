// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package dataquality

import (
	"net/http"

	"backend/internal/logic/core/dataquality"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 请求 UNS 节点立即进入深度质量检测
func DataQualityRunCreateHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DataQualityNodeReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataquality.NewDataQualityRunCreateLogic(r.Context(), svcCtx)
		resp, err := l.DataQualityRunCreate(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
