// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordDeleteOneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordDeleteOneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordDeleteOneLogic {
	return &UnsV2RecordDeleteOneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordDeleteOneLogic) UnsV2RecordDeleteOne(req *types.UnsV2RecordDeleteOneReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
