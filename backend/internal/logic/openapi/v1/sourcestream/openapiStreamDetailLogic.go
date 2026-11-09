// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiStreamDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiStreamDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiStreamDetailLogic {
	return &OpenapiStreamDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiStreamDetailLogic) OpenapiStreamDetail(req *types.StreamIDReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiStreamDetail", req)
}
