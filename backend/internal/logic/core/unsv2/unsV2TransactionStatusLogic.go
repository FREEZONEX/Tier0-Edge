// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2TransactionStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2TransactionStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2TransactionStatusLogic {
	return &UnsV2TransactionStatusLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2TransactionStatusLogic) UnsV2TransactionStatus(req *types.UnsV2TransactionStatusReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
