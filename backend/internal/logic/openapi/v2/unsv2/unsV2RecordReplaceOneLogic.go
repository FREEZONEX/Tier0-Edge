// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2RecordReplaceOneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2RecordReplaceOneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2RecordReplaceOneLogic {
	return &UnsV2RecordReplaceOneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2RecordReplaceOneLogic) UnsV2RecordReplaceOne(req *types.UnsV2RecordReplaceOneReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
