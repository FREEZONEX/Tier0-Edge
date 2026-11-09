// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetMultipartPartUrlsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetMultipartPartUrlsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetMultipartPartUrlsLogic {
	return &OpenapiAssetMultipartPartUrlsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetMultipartPartUrlsLogic) OpenapiAssetMultipartPartUrls(req *types.OpenapiAssetMultipartPartUrlsReq) (resp *types.Envelope, err error) {
	results, err := l.svcCtx.App.Asset.MultipartPartURLs(l.ctx, req.FileKey, req.UploadId, req.PartNumbers)
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, "", "", auditdomain.BizView, map[string]any{
		"fileKey":  req.FileKey,
		"uploadId": req.UploadId,
		"count":    len(results),
	})
	partURLs := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{
			"partNumber": r.PartNumber,
			"url":        r.URL,
		}
		if r.ExpiresAt > 0 {
			item["expiresAt"] = r.ExpiresAt
		}
		partURLs = append(partURLs, item)
	}
	return respx.Envelope(map[string]any{"partUrls": partURLs}), nil
}
