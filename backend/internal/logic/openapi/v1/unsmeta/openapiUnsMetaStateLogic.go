// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiUnsMetaStateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiUnsMetaStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiUnsMetaStateLogic {
	return &OpenapiUnsMetaStateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiUnsMetaStateLogic) OpenapiUnsMetaState(req *types.UnsMetaStateReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
