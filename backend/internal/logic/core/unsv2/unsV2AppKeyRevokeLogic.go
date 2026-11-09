// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppKeyRevokeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppKeyRevokeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppKeyRevokeLogic {
	return &UnsV2AppKeyRevokeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppKeyRevokeLogic) UnsV2AppKeyRevoke(req *types.UnsV2AppKeyRevokeReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
