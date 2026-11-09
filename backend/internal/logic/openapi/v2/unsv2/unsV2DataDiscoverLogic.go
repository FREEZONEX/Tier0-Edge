// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2DataDiscoverLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2DataDiscoverLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2DataDiscoverLogic {
	return &UnsV2DataDiscoverLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2DataDiscoverLogic) UnsV2DataDiscover(req *types.UnsV2DiscoverySearchReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
