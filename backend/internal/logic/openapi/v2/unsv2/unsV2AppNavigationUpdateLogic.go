// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppNavigationUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppNavigationUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppNavigationUpdateLogic {
	return &UnsV2AppNavigationUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppNavigationUpdateLogic) UnsV2AppNavigationUpdate(req *types.UnsV2AppNavigationUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
