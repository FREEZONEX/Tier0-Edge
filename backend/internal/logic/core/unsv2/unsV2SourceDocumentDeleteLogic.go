// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceDocumentDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceDocumentDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceDocumentDeleteLogic {
	return &UnsV2SourceDocumentDeleteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceDocumentDeleteLogic) UnsV2SourceDocumentDelete(req *types.UnsV2SourceDocumentDeleteReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
