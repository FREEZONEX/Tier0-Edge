// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataReplaceOneLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataReplaceOneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataReplaceOneLogic {
	return &UnsV2MetadataReplaceOneLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataReplaceOneLogic) UnsV2MetadataReplaceOne(req *types.UnsV2MetadataReplaceOneReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
