// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package uns

import (
	"context"

	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsNodeCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsNodeCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsNodeCreateLogic {
	return &UnsNodeCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnsNodeCreateLogic) UnsNodeCreate(req *types.UnsNodeSaveReq) (resp *types.Envelope, err error) {
	if err := authorizeDataQualityPolicy(l.ctx, req); err != nil {
		return nil, err
	}
	data, err := l.svcCtx.App.UNS.Create(l.ctx, BuildUnsNodeSaveCommand(l.ctx, req))
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordUNSAudit(l.ctx, l.svcCtx, data, auditdomain.BizCreate)
	return respx.Envelope(data), nil
}
