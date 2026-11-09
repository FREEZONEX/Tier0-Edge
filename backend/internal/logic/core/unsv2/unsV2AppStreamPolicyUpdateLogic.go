// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2AppStreamPolicyUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2AppStreamPolicyUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2AppStreamPolicyUpdateLogic {
	return &UnsV2AppStreamPolicyUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2AppStreamPolicyUpdateLogic) UnsV2AppStreamPolicyUpdate(req *types.UnsV2AppStreamPolicyUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
