// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppPlatformPermissionsUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppPlatformPermissionsUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppPlatformPermissionsUpdateLogic {
	return &UnsV2AppPlatformPermissionsUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppPlatformPermissionsUpdateLogic) UnsV2AppPlatformPermissionsUpdate(req *types.UnsV2AppPlatformPermissionsUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
