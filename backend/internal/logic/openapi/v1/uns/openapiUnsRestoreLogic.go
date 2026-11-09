// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package uns

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiUnsRestoreLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiUnsRestoreLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiUnsRestoreLogic {
	return &OpenapiUnsRestoreLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiUnsRestoreLogic) OpenapiUnsRestore(req *types.NodeRestoreReq) (resp *types.Envelope, err error) {
	return openapiUnsRestore(l.ctx, l.svcCtx, req)
}
