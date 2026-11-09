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

// 查询 UNS 节点数据质量策略
func DataQualityPolicyGetHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DataQualityNodeReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataquality.NewDataQualityPolicyGetLogic(r.Context(), svcCtx)
		resp, err := l.DataQualityPolicyGet(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
