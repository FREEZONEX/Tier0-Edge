// Code scaffolded by goctl. Safe to edit.
// goctl <no value>
package unsmeta

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsCollectionCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsCollectionCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsCollectionCreateLogic {
	return &UnsCollectionCreateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsCollectionCreateLogic) UnsCollectionCreate(req *types.UnsCollectionCreateReq) (resp *types.UnsMetaEnvelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
