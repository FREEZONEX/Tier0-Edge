// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceProtocolsListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceProtocolsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceProtocolsListLogic {
	return &UnsV2SourceProtocolsListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceProtocolsListLogic) UnsV2SourceProtocolsList(req *types.UnsV2SourceProtocolsListReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
