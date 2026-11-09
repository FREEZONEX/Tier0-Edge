// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamProgramVersionsListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamProgramVersionsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamProgramVersionsListLogic {
	return &UnsV2StreamProgramVersionsListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamProgramVersionsListLogic) UnsV2StreamProgramVersionsList(req *types.UnsV2StreamProgramVersionsListReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
