// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiSourceOAuthStartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiSourceOAuthStartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiSourceOAuthStartLogic {
	return &OpenapiSourceOAuthStartLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiSourceOAuthStartLogic) OpenapiSourceOAuthStart(req *types.SourceOAuthStartReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiSourceOAuthStart", req)
}
