// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2StreamDocumentCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2StreamDocumentCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2StreamDocumentCreateLogic {
	return &UnsV2StreamDocumentCreateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2StreamDocumentCreateLogic) UnsV2StreamDocumentCreate(req *types.CloudStreamDocumentCreateReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
