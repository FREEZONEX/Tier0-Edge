// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataCountLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataCountLogic {
	return &UnsV2MetadataCountLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataCountLogic) UnsV2MetadataCount(req *types.UnsV2MetadataCountReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
