// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceNativeOAuthCompleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceNativeOAuthCompleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceNativeOAuthCompleteLogic {
	return &UnsV2SourceNativeOAuthCompleteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceNativeOAuthCompleteLogic) UnsV2SourceNativeOAuthComplete(req *types.UnsV2SourceNativeOAuthCompleteReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
