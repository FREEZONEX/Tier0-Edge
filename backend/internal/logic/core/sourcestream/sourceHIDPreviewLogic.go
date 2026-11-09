// Code scaffolded by goctl. Safe to edit.
// goctl 1.7.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SourceHIDPreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSourceHIDPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SourceHIDPreviewLogic {
	return &SourceHIDPreviewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SourceHIDPreviewLogic) SourceHIDPreview(req *types.SourceHIDPreviewReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "SourceHIDPreview", req)
}
