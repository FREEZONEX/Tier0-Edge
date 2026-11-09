// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppMembersUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppMembersUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppMembersUpdateLogic {
	return &UnsV2AppMembersUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppMembersUpdateLogic) UnsV2AppMembersUpdate(req *types.UnsV2AppMembersUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
