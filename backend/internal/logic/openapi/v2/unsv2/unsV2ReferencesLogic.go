// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2ReferencesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2ReferencesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2ReferencesLogic {
	return &UnsV2ReferencesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2ReferencesLogic) UnsV2References(req *types.UnsV2QueryReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
