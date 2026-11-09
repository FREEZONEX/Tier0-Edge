// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceDocumentUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceDocumentUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceDocumentUpdateLogic {
	return &UnsV2SourceDocumentUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceDocumentUpdateLogic) UnsV2SourceDocumentUpdate(req *types.CloudSourceDocumentUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
