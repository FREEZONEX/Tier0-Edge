// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceRuntimeGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceRuntimeGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceRuntimeGetLogic {
	return &UnsV2SourceRuntimeGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceRuntimeGetLogic) UnsV2SourceRuntimeGet(req *types.UnsV2SourceRuntimeGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
