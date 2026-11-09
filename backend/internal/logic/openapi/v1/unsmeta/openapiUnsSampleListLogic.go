// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiUnsSampleListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiUnsSampleListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiUnsSampleListLogic {
	return &OpenapiUnsSampleListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiUnsSampleListLogic) OpenapiUnsSampleList(req *types.OpenapiUnsSampleListReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
