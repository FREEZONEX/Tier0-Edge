// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SampleLatestValuesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SampleLatestValuesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SampleLatestValuesLogic {
	return &UnsV2SampleLatestValuesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SampleLatestValuesLogic) UnsV2SampleLatestValues(req *types.UnsV2SampleLatestValuesReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
