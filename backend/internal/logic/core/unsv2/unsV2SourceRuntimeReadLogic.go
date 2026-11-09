// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceRuntimeReadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceRuntimeReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceRuntimeReadLogic {
	return &UnsV2SourceRuntimeReadLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceRuntimeReadLogic) UnsV2SourceRuntimeRead(req *types.UnsV2SourceRuntimeReadReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
