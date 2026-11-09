package iam

import (
	domainiam "backend/internal/domain/iam"
	"backend/internal/types"
)

func toUserExportRequest(req *types.UserExportReq) domainiam.UserExportRequest {
	if req == nil {
		return domainiam.UserExportRequest{}
	}
	return domainiam.UserExportRequest{
		Scope: req.Scope, UserIDs: req.UserIds, GroupIDs: req.GroupIds,
		Keyword: req.Keyword, Statuses: req.Statuses, RoleIDs: req.RoleIds,
		SensitiveMode: req.SensitiveMode,
	}
}
