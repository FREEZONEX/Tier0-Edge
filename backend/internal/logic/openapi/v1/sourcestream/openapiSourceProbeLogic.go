// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiSourceProbeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiSourceProbeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiSourceProbeLogic {
	return &OpenapiSourceProbeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiSourceProbeLogic) OpenapiSourceProbe(req *types.SourceProbeReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiSourceProbe", req)
}
