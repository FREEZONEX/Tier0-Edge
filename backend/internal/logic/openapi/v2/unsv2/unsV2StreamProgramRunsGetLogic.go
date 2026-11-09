// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamProgramRunsGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamProgramRunsGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamProgramRunsGetLogic {
	return &UnsV2StreamProgramRunsGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamProgramRunsGetLogic) UnsV2StreamProgramRunsGet(req *types.UnsV2StreamProgramRunsGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
