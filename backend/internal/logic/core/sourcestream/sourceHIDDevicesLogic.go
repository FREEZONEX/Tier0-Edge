// Code scaffolded by goctl. Safe to edit.
// goctl 1.7.1
package sourcestream

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SourceHIDDevicesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSourceHIDDevicesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SourceHIDDevicesLogic {
	return &SourceHIDDevicesLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *SourceHIDDevicesLogic) SourceHIDDevices(req *types.SourceStreamEmpty) (resp *types.SourceStreamEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, "SourceHIDDevices", req)
}
