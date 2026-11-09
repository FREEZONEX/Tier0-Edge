// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SourceOAuthCompleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSourceOAuthCompleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SourceOAuthCompleteLogic {
	return &SourceOAuthCompleteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SourceOAuthCompleteLogic) SourceOAuthComplete(req *types.SourceOAuthCompleteReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "SourceOAuthComplete", req)
}
