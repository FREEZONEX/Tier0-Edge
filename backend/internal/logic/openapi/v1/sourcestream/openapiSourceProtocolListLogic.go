// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiSourceProtocolListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiSourceProtocolListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiSourceProtocolListLogic {
	return &OpenapiSourceProtocolListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiSourceProtocolListLogic) OpenapiSourceProtocolList(req *types.SourceStreamEmpty) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiSourceProtocolList", req)
}
