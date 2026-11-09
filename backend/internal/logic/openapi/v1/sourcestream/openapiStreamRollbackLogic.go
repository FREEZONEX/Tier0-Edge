// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiStreamRollbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiStreamRollbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiStreamRollbackLogic {
	return &OpenapiStreamRollbackLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiStreamRollbackLogic) OpenapiStreamRollback(req *types.StreamRollbackReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiStreamRollback", req)
}
