// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceNativeOAuthStartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceNativeOAuthStartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceNativeOAuthStartLogic {
	return &UnsV2SourceNativeOAuthStartLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceNativeOAuthStartLogic) UnsV2SourceNativeOAuthStart(req *types.UnsV2SourceNativeOAuthStartReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
