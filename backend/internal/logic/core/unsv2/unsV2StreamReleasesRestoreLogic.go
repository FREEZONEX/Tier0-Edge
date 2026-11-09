// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamReleasesRestoreLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamReleasesRestoreLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamReleasesRestoreLogic {
	return &UnsV2StreamReleasesRestoreLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamReleasesRestoreLogic) UnsV2StreamReleasesRestore(req *types.UnsV2StreamReleasesRestoreReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
