// Code scaffolded by goctl. Safe to edit.
package streamrequests

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamRequestDescribeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStreamRequestDescribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamRequestDescribeLogic {
	return &StreamRequestDescribeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StreamRequestDescribeLogic) StreamRequestDescribe(req *types.StreamRequestDescribeReq) (any, error) {
	return Execute(l.ctx, l.svcCtx, req)
}
