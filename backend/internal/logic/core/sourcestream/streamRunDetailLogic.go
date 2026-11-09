// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamRunDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStreamRunDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamRunDetailLogic {
	return &StreamRunDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StreamRunDetailLogic) StreamRunDetail(req *types.StreamRunIDReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "StreamRunDetail", req)
}
