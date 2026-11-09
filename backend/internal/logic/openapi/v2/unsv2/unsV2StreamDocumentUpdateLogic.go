// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamDocumentUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamDocumentUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamDocumentUpdateLogic {
	return &UnsV2StreamDocumentUpdateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamDocumentUpdateLogic) UnsV2StreamDocumentUpdate(req *types.CloudStreamDocumentUpdateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
