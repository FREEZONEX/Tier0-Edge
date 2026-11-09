// Code scaffolded by goctl. Safe to edit.
package streamrequests

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamRequestDiscoverLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStreamRequestDiscoverLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamRequestDiscoverLogic {
	return &StreamRequestDiscoverLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StreamRequestDiscoverLogic) StreamRequestDiscover(req *types.StreamRequestDiscoverReq) (any, error) {
	return Execute(l.ctx, l.svcCtx, req)
}
