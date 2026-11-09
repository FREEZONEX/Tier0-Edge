// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamReleasesGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamReleasesGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamReleasesGetLogic {
	return &UnsV2StreamReleasesGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamReleasesGetLogic) UnsV2StreamReleasesGet(req *types.UnsV2StreamReleasesGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
