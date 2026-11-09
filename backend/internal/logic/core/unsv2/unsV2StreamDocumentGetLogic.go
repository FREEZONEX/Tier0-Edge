// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamDocumentGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamDocumentGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamDocumentGetLogic {
	return &UnsV2StreamDocumentGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamDocumentGetLogic) UnsV2StreamDocumentGet(req *types.CloudStreamDocumentGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
