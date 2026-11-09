// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package dataquality

import (
	"net/http"

	"backend/internal/logic/core/dataquality"
	"backend/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 查询 UNS 树活动数据质量异常摘要
func DataQualityActiveSummaryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := dataquality.NewDataQualityActiveSummaryLogic(r.Context(), svcCtx)
		resp, err := l.DataQualityActiveSummary()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
