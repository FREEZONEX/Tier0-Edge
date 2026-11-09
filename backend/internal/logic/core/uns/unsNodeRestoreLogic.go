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

type UnsNodeRestoreLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsNodeRestoreLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsNodeRestoreLogic {
	return &UnsNodeRestoreLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnsNodeRestoreLogic) UnsNodeRestore(req *types.UnsRestoreReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.UNS.Restore(l.ctx, req.NodeId, logicx.UserID(l.ctx), req.Confirm)
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordUNSAudit(l.ctx, l.svcCtx, data, auditdomain.BizRestore)
	return respx.Envelope(data), nil
}
