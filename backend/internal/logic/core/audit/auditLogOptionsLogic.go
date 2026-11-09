// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package audit

import (
	respx "backend/internal/httpx"
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuditLogOptionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuditLogOptionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuditLogOptionsLogic {
	return &AuditLogOptionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuditLogOptionsLogic) AuditLogOptions(req *types.AuditLogPageReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Audit.Options(l.ctx, toPageQuery(req))
	if err != nil {
		return nil, queryError(err)
	}

	return respx.Envelope(data), nil
}
