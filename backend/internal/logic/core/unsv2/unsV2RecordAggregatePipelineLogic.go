// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordAggregatePipelineLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordAggregatePipelineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordAggregatePipelineLogic {
	return &UnsV2RecordAggregatePipelineLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordAggregatePipelineLogic) UnsV2RecordAggregatePipeline(req *types.UnsV2RecordAggregatePipelineReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
