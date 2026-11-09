// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordQueryBatchLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordQueryBatchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordQueryBatchLogic {
	return &UnsV2RecordQueryBatchLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordQueryBatchLogic) UnsV2RecordQueryBatch(req *types.UnsV2RecordQueryBatchReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
