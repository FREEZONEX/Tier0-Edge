// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamRequestGrantGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamRequestGrantGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamRequestGrantGetLogic {
	return &UnsV2StreamRequestGrantGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamRequestGrantGetLogic) UnsV2StreamRequestGrantGet(req *types.UnsV2StreamRequestGrantGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
