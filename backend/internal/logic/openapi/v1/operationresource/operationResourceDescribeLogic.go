// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package operationresource

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OperationResourceDescribeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// OperationResourceDescribe
func NewOperationResourceDescribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OperationResourceDescribeLogic {
	return &OperationResourceDescribeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OperationResourceDescribeLogic) OperationResourceDescribe(req *types.OperationResourceDetailReq) (resp *types.OperationResourceEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OperationResourceDescribe", req)
}
