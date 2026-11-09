// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceResourceReadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceResourceReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceResourceReadLogic {
	return &UnsV2SourceResourceReadLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceResourceReadLogic) UnsV2SourceResourceRead(req *types.CloudSourceResourceReadReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
