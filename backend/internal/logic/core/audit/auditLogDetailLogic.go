// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package audit

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuditLogDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuditLogDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuditLogDetailLogic {
	return &AuditLogDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuditLogDetailLogic) AuditLogDetail(req *types.IdReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Audit.Detail(l.ctx, req.Id)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
