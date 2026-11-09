// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package audit

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuditLogPageLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuditLogPageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuditLogPageLogic {
	return &AuditLogPageLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuditLogPageLogic) AuditLogPage(req *types.AuditLogPageReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Audit.Page(l.ctx, toPageQuery(req))
	if err != nil {
		return nil, queryError(err)
	}
	return respx.Envelope(data), nil
}
