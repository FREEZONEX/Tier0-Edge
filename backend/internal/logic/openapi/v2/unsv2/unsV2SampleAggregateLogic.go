// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SampleAggregateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SampleAggregateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SampleAggregateLogic {
	return &UnsV2SampleAggregateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SampleAggregateLogic) UnsV2SampleAggregate(req *types.UnsV2SampleAggregateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
