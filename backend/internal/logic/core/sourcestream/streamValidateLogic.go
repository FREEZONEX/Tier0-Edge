// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type StreamValidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStreamValidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StreamValidateLogic {
	return &StreamValidateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *StreamValidateLogic) StreamValidate(req *types.StreamValidateReq) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "StreamValidate", req)
}
