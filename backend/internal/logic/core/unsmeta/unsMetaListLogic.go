// Code scaffolded by goctl. Safe to edit.
// goctl <no value>
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsMetaListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsMetaListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsMetaListLogic {
	return &UnsMetaListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsMetaListLogic) UnsMetaList(req *types.UnsMetaListReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
