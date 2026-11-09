// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiStreamUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiStreamUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiStreamUpdateLogic {
	return &OpenapiStreamUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiStreamUpdateLogic) OpenapiStreamUpdate(req *types.StreamSaveReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiStreamUpdate", req)
}
