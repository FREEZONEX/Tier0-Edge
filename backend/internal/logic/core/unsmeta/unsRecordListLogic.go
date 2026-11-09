// Code scaffolded by goctl. Safe to edit.
// goctl <no value>
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsRecordListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsRecordListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsRecordListLogic {
	return &UnsRecordListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsRecordListLogic) UnsRecordList(req *types.UnsRecordListReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
