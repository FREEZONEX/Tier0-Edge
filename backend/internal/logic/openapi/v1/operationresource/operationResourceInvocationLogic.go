// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package operationresource

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OperationResourceInvocationLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// OperationResourceInvocation
func NewOperationResourceInvocationLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OperationResourceInvocationLogic {
	return &OperationResourceInvocationLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OperationResourceInvocationLogic) OperationResourceInvocation(req *types.OperationResourceInvocationReq) (resp *types.OperationResourceEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OperationResourceInvocation", req)
}
