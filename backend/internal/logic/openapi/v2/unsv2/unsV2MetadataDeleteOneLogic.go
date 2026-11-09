// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataDeleteOneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataDeleteOneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataDeleteOneLogic {
	return &UnsV2MetadataDeleteOneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataDeleteOneLogic) UnsV2MetadataDeleteOne(req *types.UnsV2MetadataDeleteOneReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
