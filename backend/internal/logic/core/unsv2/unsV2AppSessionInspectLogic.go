// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppSessionInspectLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppSessionInspectLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppSessionInspectLogic {
	return &UnsV2AppSessionInspectLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppSessionInspectLogic) UnsV2AppSessionInspect(req *types.UnsV2AppSessionInspectReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
