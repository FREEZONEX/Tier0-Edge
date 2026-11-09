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

// 查询数据质量 Issue
func DataQualityIssueListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DataQualityIssueListReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataquality.NewDataQualityIssueListLogic(r.Context(), svcCtx)
		resp, err := l.DataQualityIssueList(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
