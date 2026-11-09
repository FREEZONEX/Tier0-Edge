// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SourceCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSourceCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SourceCreateLogic {
	return &SourceCreateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SourceCreateLogic) SourceCreate(req *types.SourceSaveReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "SourceCreate", req)
}
