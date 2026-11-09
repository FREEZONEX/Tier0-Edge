// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamDraftUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamDraftUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamDraftUpdateLogic {
	return &UnsV2StreamDraftUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamDraftUpdateLogic) UnsV2StreamDraftUpdate(req *types.UnsV2StreamDraftUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
