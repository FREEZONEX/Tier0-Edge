// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetDetailLogic {
	return &OpenapiAssetDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetDetailLogic) OpenapiAssetDetail(req *types.FileIdReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Asset.Detail(l.ctx, req.FileId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
