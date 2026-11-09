// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppStreamAccessListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppStreamAccessListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppStreamAccessListLogic {
	return &UnsV2AppStreamAccessListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppStreamAccessListLogic) UnsV2AppStreamAccessList(req *types.UnsV2AppStreamAccessListReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
