// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceCredentialsListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceCredentialsListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceCredentialsListLogic {
	return &UnsV2SourceCredentialsListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceCredentialsListLogic) UnsV2SourceCredentialsList(req *types.UnsV2SourceCredentialsListReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
