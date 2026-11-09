// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceDocumentCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceDocumentCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceDocumentCreateLogic {
	return &UnsV2SourceDocumentCreateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceDocumentCreateLogic) UnsV2SourceDocumentCreate(req *types.CloudSourceDocumentCreateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
