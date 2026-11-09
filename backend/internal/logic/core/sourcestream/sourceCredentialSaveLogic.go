// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SourceCredentialSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSourceCredentialSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SourceCredentialSaveLogic {
	return &SourceCredentialSaveLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SourceCredentialSaveLogic) SourceCredentialSave(req *types.SourceCredentialSaveReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "SourceCredentialSave", req)
}
