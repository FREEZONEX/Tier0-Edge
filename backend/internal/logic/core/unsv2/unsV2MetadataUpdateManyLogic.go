// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataUpdateManyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataUpdateManyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataUpdateManyLogic {
	return &UnsV2MetadataUpdateManyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataUpdateManyLogic) UnsV2MetadataUpdateMany(req *types.UnsV2MetadataUpdateManyReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
