// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataRestoreLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataRestoreLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataRestoreLogic {
	return &UnsV2MetadataRestoreLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataRestoreLogic) UnsV2MetadataRestore(req *types.UnsV2MetadataRestoreReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
