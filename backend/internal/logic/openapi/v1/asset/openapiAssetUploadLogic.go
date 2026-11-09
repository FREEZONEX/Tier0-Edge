// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	domainasset "backend/internal/domain/asset"
	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetUploadLogic {
	return &OpenapiAssetUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetUploadLogic) OpenapiAssetUpload(req *types.AssetUploadReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Asset.Upload(l.ctx, domainasset.UploadCommand{
		FileName:    req.FileName,
		ContentType: req.ContentType,
		Size:        req.Size,
		Sha256:      req.Sha256,
		StorageKey:  req.StorageKey,
		UserID:      logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, "", req.FileName, auditdomain.BizCreate, map[string]any{
		"fileName":    req.FileName,
		"filePath":    data["filePath"],
		"size":        req.Size,
		"contentType": req.ContentType,
		"sha256":      req.Sha256,
	})
	return respx.Envelope(map[string]any{
		"uploadUrl": "",
		"filePath":  data["filePath"],
		"fileUrl":   data["fileUrl"],
	}), nil
}
