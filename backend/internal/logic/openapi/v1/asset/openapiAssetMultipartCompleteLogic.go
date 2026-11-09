// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/infra/filestore"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetMultipartCompleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetMultipartCompleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetMultipartCompleteLogic {
	return &OpenapiAssetMultipartCompleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetMultipartCompleteLogic) OpenapiAssetMultipartComplete(req *types.OpenapiAssetMultipartCompleteReq) (resp *types.Envelope, err error) {
	parts := make([]filestore.CompletePart, 0, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, filestore.CompletePart{
			PartNumber: p.PartNumber,
			ETag:       p.Etag,
		})
	}
	result, err := l.svcCtx.App.Asset.MultipartComplete(l.ctx, req.FileKey, req.UploadId, parts)
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, "", "", auditdomain.BizCreate, map[string]any{
		"fileKey":   req.FileKey,
		"uploadId":  req.UploadId,
		"filePath":  result.FilePath,
		"sizeBytes": result.SizeBytes,
	})
	data := map[string]any{
		"filePath":  result.FilePath,
		"fileUrl":   result.FileURL,
		"sizeBytes": result.SizeBytes,
	}
	if result.ExpiresAt > 0 {
		data["expiresAt"] = result.ExpiresAt
	}
	return respx.Envelope(data), nil
}
