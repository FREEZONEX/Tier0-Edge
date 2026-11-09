// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordDeleteManyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordDeleteManyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordDeleteManyLogic {
	return &UnsV2RecordDeleteManyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordDeleteManyLogic) UnsV2RecordDeleteMany(req *types.UnsV2RecordDeleteManyReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
