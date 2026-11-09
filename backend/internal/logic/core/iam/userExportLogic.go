package iam

import (
	"context"

	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserExportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserExportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserExportLogic {
	return &UserExportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserExportLogic) UserExport(req *types.UserExportReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.IAM.CreateUserExportJob(l.ctx, toUserExportRequest(req), logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	jobKey, _ := data["jobKey"].(string)
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeUserManagement,
		ResID:        jobKey,
		ResName:      "User export",
		BusinessType: auditdomain.BizExport,
		Detail: map[string]any{
			"jobKey":        jobKey,
			"scope":         req.Scope,
			"sensitiveMode": req.SensitiveMode,
			"userIds":       len(req.UserIds),
			"groupIds":      len(req.GroupIds),
		},
	})
	return respx.Envelope(data), nil
}
