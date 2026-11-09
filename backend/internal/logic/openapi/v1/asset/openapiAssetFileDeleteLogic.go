// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"
	"strconv"

	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetFileDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetFileDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetFileDeleteLogic {
	return &OpenapiAssetFileDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetFileDeleteLogic) OpenapiAssetFileDelete(req *types.OpenapiAssetFileDeleteReq) (resp *types.Envelope, err error) {
	item, err := l.svcCtx.App.Asset.ResolveOpenapiFile(l.ctx, req.FilePath, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	if err := l.svcCtx.App.Asset.DeleteOpenapiFile(l.ctx, item); err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, strconv.FormatInt(item.ID, 10), item.OriginalName, auditdomain.BizDelete, map[string]any{
		"filePath": req.FilePath,
		"fileName": item.OriginalName,
	})
	return respx.Envelope(map[string]any{"deleted": true}), nil
}
