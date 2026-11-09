// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package dataquality

import (
	"net/http"

	respx "backend/internal/httpx"
	"backend/internal/logic/core/dataquality"
	"backend/internal/svc"
	"backend/internal/types"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

// 确认数据质量 Issue
func DataQualityIssueAckHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DataQualityIssueIDReq
		if err := gozerohttpx.Parse(r, &req); err != nil {
			respx.WriteError(w, respx.NewHTTPError(http.StatusBadRequest, "invalid request: "+err.Error()))
			return
		}

		l := dataquality.NewDataQualityIssueAckLogic(r.Context(), svcCtx)
		resp, err := l.DataQualityIssueAck(&req)
		if err != nil {
			respx.WriteError(w, err)
		} else {
			gozerohttpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
