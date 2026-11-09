// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiStreamDeployLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiStreamDeployLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiStreamDeployLogic {
	return &OpenapiStreamDeployLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiStreamDeployLogic) OpenapiStreamDeploy(req *types.StreamDeployReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiStreamDeploy", req)
}
