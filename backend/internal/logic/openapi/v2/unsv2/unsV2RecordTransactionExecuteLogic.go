// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordTransactionExecuteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordTransactionExecuteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordTransactionExecuteLogic {
	return &UnsV2RecordTransactionExecuteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordTransactionExecuteLogic) UnsV2RecordTransactionExecute(req *types.UnsV2RecordTransactionReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
