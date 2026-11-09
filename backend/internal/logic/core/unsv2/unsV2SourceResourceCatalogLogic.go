// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceResourceCatalogLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceResourceCatalogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceResourceCatalogLogic {
	return &UnsV2SourceResourceCatalogLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceResourceCatalogLogic) UnsV2SourceResourceCatalog(req *types.CloudSourceResourceCatalogReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
