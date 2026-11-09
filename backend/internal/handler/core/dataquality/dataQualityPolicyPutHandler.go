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

// 创建或更新 UNS 节点数据质量策略
func DataQualityPolicyPutHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DataQualityPolicyReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataquality.NewDataQualityPolicyPutLogic(r.Context(), svcCtx)
		resp, err := l.DataQualityPolicyPut(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
