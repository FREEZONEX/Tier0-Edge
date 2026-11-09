// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiSourceCredentialSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiSourceCredentialSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiSourceCredentialSaveLogic {
	return &OpenapiSourceCredentialSaveLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *OpenapiSourceCredentialSaveLogic) OpenapiSourceCredentialSave(req *types.SourceCredentialSaveReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "OpenapiSourceCredentialSave", req)
}
