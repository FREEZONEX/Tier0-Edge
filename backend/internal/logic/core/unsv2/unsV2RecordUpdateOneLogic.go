// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordUpdateOneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordUpdateOneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordUpdateOneLogic {
	return &UnsV2RecordUpdateOneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordUpdateOneLogic) UnsV2RecordUpdateOne(req *types.UnsV2RecordUpdateOneReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
