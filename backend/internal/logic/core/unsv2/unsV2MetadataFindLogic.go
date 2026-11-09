// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataFindLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataFindLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataFindLogic {
	return &UnsV2MetadataFindLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataFindLogic) UnsV2MetadataFind(req *types.UnsV2MetadataFindReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
