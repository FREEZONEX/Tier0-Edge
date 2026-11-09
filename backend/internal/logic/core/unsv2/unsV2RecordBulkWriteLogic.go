// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordBulkWriteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordBulkWriteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordBulkWriteLogic {
	return &UnsV2RecordBulkWriteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordBulkWriteLogic) UnsV2RecordBulkWrite(req *types.UnsV2RecordBulkWriteReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
