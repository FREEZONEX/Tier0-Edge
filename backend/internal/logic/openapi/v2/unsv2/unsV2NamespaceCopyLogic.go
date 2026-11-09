// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2NamespaceCopyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2NamespaceCopyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2NamespaceCopyLogic {
	return &UnsV2NamespaceCopyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2NamespaceCopyLogic) UnsV2NamespaceCopy(req *types.UnsV2NamespaceCopyReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
