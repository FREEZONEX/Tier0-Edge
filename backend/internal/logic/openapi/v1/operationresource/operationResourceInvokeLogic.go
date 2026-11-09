// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package operationresource

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OperationResourceInvokeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// OperationResourceInvoke
func NewOperationResourceInvokeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OperationResourceInvokeLogic {
	return &OperationResourceInvokeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OperationResourceInvokeLogic) OperationResourceInvoke(req *types.OperationResourceInvokeReq) (resp *types.OperationResourceEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OperationResourceInvoke", req)
}
