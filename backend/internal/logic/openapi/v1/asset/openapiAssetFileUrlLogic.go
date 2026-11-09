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

type OpenapiAssetFileUrlLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetFileUrlLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetFileUrlLogic {
	return &OpenapiAssetFileUrlLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetFileUrlLogic) OpenapiAssetFileUrl(req *types.OpenapiAssetFileUrlReq) (resp *types.Envelope, err error) {
	item, err := l.svcCtx.App.Asset.ResolveOpenapiFile(l.ctx, req.FilePath, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	result, err := l.svcCtx.App.Asset.OpenapiFileURL(l.ctx, item, req.ExpiredSec)
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, strconv.FormatInt(item.ID, 10), item.OriginalName, auditdomain.BizView, map[string]any{
		"filePath":   req.FilePath,
		"fileName":   item.OriginalName,
		"expiredSec": req.ExpiredSec,
	})
	data := map[string]any{"fileUrl": result.FileURL}
	if result.ExpiresAt > 0 {
		data["expiresAt"] = result.ExpiresAt
	}
	return respx.Envelope(data), nil
}
