// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiTopicListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiTopicListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiTopicListLogic {
	return &OpenapiTopicListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiTopicListLogic) OpenapiTopicList(req *types.OpenapiTopicListReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
