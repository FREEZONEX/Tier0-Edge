// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiTopicValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiTopicValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiTopicValidateLogic {
	return &OpenapiTopicValidateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiTopicValidateLogic) OpenapiTopicValidate(req *types.UnsTopicValidateReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
