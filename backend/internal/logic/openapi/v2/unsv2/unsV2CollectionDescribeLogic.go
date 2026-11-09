// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2CollectionDescribeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2CollectionDescribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2CollectionDescribeLogic {
	return &UnsV2CollectionDescribeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2CollectionDescribeLogic) UnsV2CollectionDescribe(req *types.UnsV2CollectionDescribeReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
