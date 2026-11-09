// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataUpdateOneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataUpdateOneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataUpdateOneLogic {
	return &UnsV2MetadataUpdateOneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataUpdateOneLogic) UnsV2MetadataUpdateOne(req *types.UnsV2MetadataUpdateOneReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
