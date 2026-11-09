// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataBulkWriteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataBulkWriteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataBulkWriteLogic {
	return &UnsV2MetadataBulkWriteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataBulkWriteLogic) UnsV2MetadataBulkWrite(req *types.UnsV2MetadataBulkWriteReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
