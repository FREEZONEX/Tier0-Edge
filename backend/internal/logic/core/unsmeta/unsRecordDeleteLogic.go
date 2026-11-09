// Code scaffolded by goctl. Safe to edit.
// goctl <no value>
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsRecordDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsRecordDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsRecordDeleteLogic {
	return &UnsRecordDeleteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsRecordDeleteLogic) UnsRecordDelete(req *types.UnsRecordDeleteReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
