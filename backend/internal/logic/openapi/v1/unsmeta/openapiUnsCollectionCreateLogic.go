// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiUnsCollectionCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiUnsCollectionCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiUnsCollectionCreateLogic {
	return &OpenapiUnsCollectionCreateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiUnsCollectionCreateLogic) OpenapiUnsCollectionCreate(req *types.UnsCollectionCreateReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
