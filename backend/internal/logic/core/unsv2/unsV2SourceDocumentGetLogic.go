// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2SourceDocumentGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2SourceDocumentGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2SourceDocumentGetLogic {
	return &UnsV2SourceDocumentGetLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2SourceDocumentGetLogic) UnsV2SourceDocumentGet(req *types.CloudSourceDocumentGetReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
