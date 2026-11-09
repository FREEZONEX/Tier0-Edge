// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceResourceScanLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceResourceScanLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceResourceScanLogic {
	return &UnsV2SourceResourceScanLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceResourceScanLogic) UnsV2SourceResourceScan(req *types.CloudSourceResourceScanReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
