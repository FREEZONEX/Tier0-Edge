// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package operationresource

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OperationResourceListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// OperationResourceList
func NewOperationResourceListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OperationResourceListLogic {
	return &OperationResourceListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OperationResourceListLogic) OperationResourceList(req *types.OperationResourceListReq) (resp *types.OperationResourceEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OperationResourceList", req)
}
