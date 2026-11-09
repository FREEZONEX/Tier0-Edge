// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiSourceDiscoverLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiSourceDiscoverLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiSourceDiscoverLogic {
	return &OpenapiSourceDiscoverLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiSourceDiscoverLogic) OpenapiSourceDiscover(req *types.SourceDiscoverReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiSourceDiscover", req)
}
