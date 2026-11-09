// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceObservationsGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceObservationsGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceObservationsGetLogic {
	return &UnsV2SourceObservationsGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceObservationsGetLogic) UnsV2SourceObservationsGet(req *types.CloudSourceObservationsGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
