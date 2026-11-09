package audit

import (
	"errors"
	"net/http"

	domainaudit "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/types"
)

func toPageQuery(req *types.AuditLogPageReq) domainaudit.PageQuery {
	var code *int
	if req.Code != 0 {
		code = &req.Code
	}
	return domainaudit.PageQuery{PageNo: req.PageNo, PageSize: req.PageSize, ScopeType: req.ScopeType, ScopeID: req.ScopeId,
		ScopeName: req.ScopeName, ResType: req.ResType, BusinessType: req.BusinessType, OperatorKeyword: req.OperatorKeyword,
		OperatorID: req.OperatorId, ResourceKeyword: req.ResourceKeyword, ResID: req.ResId, RequestID: req.RequestId, IP: req.Ip, Result: req.Result,
		Code: code, StartTime: req.StartTime, EndTime: req.EndTime}
}

func queryError(err error) error {
	if errors.Is(err, domainaudit.ErrInvalidFilter) || errors.Is(err, domainaudit.ErrExportLimit) {
		return respx.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return logicx.Error(err)
}
