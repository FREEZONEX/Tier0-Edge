// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamProgramRunsListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamProgramRunsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamProgramRunsListLogic {
	return &UnsV2StreamProgramRunsListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamProgramRunsListLogic) UnsV2StreamProgramRunsList(req *types.UnsV2StreamProgramRunsListReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
