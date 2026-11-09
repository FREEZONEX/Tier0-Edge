// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamRequestGrantSetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamRequestGrantSetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamRequestGrantSetLogic {
	return &UnsV2StreamRequestGrantSetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamRequestGrantSetLogic) UnsV2StreamRequestGrantSet(req *types.UnsV2StreamRequestGrantSetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
