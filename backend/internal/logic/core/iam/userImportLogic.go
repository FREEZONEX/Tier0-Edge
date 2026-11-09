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

type UserImportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserImportLogic {
	return &UserImportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserImportLogic) UserImport(req *types.UserImportReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.IAM.CreateUserImportJob(l.ctx, req.SourceFileId, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	jobKey, _ := data["jobKey"].(string)
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeUserManagement,
		ResID:        jobKey,
		ResName:      "User import",
		BusinessType: auditdomain.BizImport,
		Detail:       map[string]any{"jobKey": jobKey, "sourceFileId": req.SourceFileId},
	})
	return respx.Envelope(data), nil
}
