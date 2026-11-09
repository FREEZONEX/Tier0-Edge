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

type UnsNodeUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsNodeUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsNodeUpdateLogic {
	return &UnsNodeUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnsNodeUpdateLogic) UnsNodeUpdate(req *types.UnsNodeSaveReq) (resp *types.Envelope, err error) {
	if err := authorizeDataQualityPolicy(l.ctx, req); err != nil {
		return nil, err
	}
	data, err := l.svcCtx.App.UNS.UpdateFromConsole(l.ctx, BuildUnsNodeSaveCommand(l.ctx, req))
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordUNSAudit(l.ctx, l.svcCtx, data, auditdomain.BizUpdate)
	return respx.Envelope(data), nil
}
