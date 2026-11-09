// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordRestoreLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordRestoreLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordRestoreLogic {
	return &UnsV2RecordRestoreLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordRestoreLogic) UnsV2RecordRestore(req *types.UnsV2RecordRestoreReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
