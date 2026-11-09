// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiTopicDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiTopicDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiTopicDetailLogic {
	return &OpenapiTopicDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiTopicDetailLogic) OpenapiTopicDetail(req *types.OpenapiTopicDetailReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
