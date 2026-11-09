// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceImpactLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceImpactLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceImpactLogic {
	return &UnsV2SourceImpactLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceImpactLogic) UnsV2SourceImpact(req *types.CloudSourceImpactReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
