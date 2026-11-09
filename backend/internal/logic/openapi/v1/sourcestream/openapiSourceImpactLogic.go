// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiSourceImpactLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiSourceImpactLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiSourceImpactLogic {
	return &OpenapiSourceImpactLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiSourceImpactLogic) OpenapiSourceImpact(req *types.SourceIDReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiSourceImpact", req)
}
