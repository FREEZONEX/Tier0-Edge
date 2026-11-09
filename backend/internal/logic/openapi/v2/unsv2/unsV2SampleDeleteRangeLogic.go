// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SampleDeleteRangeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SampleDeleteRangeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SampleDeleteRangeLogic {
	return &UnsV2SampleDeleteRangeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SampleDeleteRangeLogic) UnsV2SampleDeleteRange(req *types.UnsV2SampleDeleteRangeReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
