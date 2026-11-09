// Code scaffolded by goctl. Safe to edit.
package streamrequests

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamRequestInvokeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStreamRequestInvokeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamRequestInvokeLogic {
	return &StreamRequestInvokeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StreamRequestInvokeLogic) StreamRequestInvoke(req *types.StreamRequestInvokeReq) (any, error) {
	return Execute(l.ctx, l.svcCtx, req)
}
