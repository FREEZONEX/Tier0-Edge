// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamReleasesListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamReleasesListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamReleasesListLogic {
	return &UnsV2StreamReleasesListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamReleasesListLogic) UnsV2StreamReleasesList(req *types.UnsV2StreamReleasesListReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
