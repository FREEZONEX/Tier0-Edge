// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamDocumentDisableLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamDocumentDisableLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamDocumentDisableLogic {
	return &UnsV2StreamDocumentDisableLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamDocumentDisableLogic) UnsV2StreamDocumentDisable(req *types.CloudStreamDocumentDisableReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
