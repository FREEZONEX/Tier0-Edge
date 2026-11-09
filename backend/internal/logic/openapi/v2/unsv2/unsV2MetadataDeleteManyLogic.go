// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataDeleteManyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataDeleteManyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataDeleteManyLogic {
	return &UnsV2MetadataDeleteManyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataDeleteManyLogic) UnsV2MetadataDeleteMany(req *types.UnsV2MetadataDeleteManyReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
