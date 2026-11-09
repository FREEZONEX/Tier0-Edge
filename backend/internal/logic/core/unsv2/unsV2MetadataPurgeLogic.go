// Code scaffolded by goctl. Safe to edit.
package unsv2

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsV2MetadataPurgeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsV2MetadataPurgeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsV2MetadataPurgeLogic {
	return &UnsV2MetadataPurgeLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}
func (l *UnsV2MetadataPurgeLogic) UnsV2MetadataPurge(req *types.UnsV2MetadataPurgeReq) (resp *types.UnsV2Envelope, err error) {
	return execute(l.ctx, l.svcCtx, req)
}
