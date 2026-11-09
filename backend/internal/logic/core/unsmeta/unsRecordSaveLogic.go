// Code scaffolded by goctl. Safe to edit.
// goctl <no value>
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsRecordSaveLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsRecordSaveLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsRecordSaveLogic {
	return &UnsRecordSaveLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsRecordSaveLogic) UnsRecordSave(req *types.UnsRecordSaveReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
