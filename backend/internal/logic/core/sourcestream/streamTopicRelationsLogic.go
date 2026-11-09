// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamTopicRelationsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStreamTopicRelationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamTopicRelationsLogic {
	return &StreamTopicRelationsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StreamTopicRelationsLogic) StreamTopicRelations(req *types.StreamTopicRelationsReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "StreamTopicRelations", req)
}
