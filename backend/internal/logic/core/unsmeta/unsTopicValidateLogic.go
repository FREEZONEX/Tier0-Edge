// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsTopicValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsTopicValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsTopicValidateLogic {
	return &UnsTopicValidateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsTopicValidateLogic) UnsTopicValidate(req *types.UnsTopicValidateReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
