// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamProgramRollbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamProgramRollbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamProgramRollbackLogic {
	return &UnsV2StreamProgramRollbackLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamProgramRollbackLogic) UnsV2StreamProgramRollback(req *types.UnsV2StreamProgramRollbackReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
