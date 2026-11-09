// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppGrantsUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppGrantsUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppGrantsUpdateLogic {
	return &UnsV2AppGrantsUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppGrantsUpdateLogic) UnsV2AppGrantsUpdate(req *types.UnsV2AppGrantsUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
