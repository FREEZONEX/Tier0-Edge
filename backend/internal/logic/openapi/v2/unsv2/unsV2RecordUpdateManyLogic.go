// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordUpdateManyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordUpdateManyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordUpdateManyLogic {
	return &UnsV2RecordUpdateManyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordUpdateManyLogic) UnsV2RecordUpdateMany(req *types.UnsV2RecordUpdateManyReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
