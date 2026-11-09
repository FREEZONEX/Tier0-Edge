// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"
	"strconv"

	domainasset "backend/internal/domain/asset"
	auditdomain "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetFileUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetFileUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetFileUploadLogic {
	return &OpenapiAssetFileUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetFileUploadLogic) OpenapiAssetFileUpload(req *types.OpenapiAssetFileUploadReq) (resp *types.Envelope, err error) {
	result, err := l.svcCtx.App.Asset.CreatePresignedUpload(l.ctx, domainasset.OpenapiUploadCommand{
		FileName:      req.FileName,
		ContentType:   req.ContentType,
		Size:          req.Size,
		UseBy:         req.UseBy,
		Visibility:    req.Visibility,
		AppInstanceID: req.AppInstanceId,
		SessionID:     req.SessionId,
		UserID:        logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, strconv.FormatInt(result.FileID, 10), req.FileName, auditdomain.BizCreate, map[string]any{
		"fileName":    req.FileName,
		"fileId":      strconv.FormatInt(result.FileID, 10),
		"filePath":    result.FilePath,
		"size":        req.Size,
		"contentType": req.ContentType,
	})
	data := map[string]any{
		"fileId":     strconv.FormatInt(result.FileID, 10),
		"filePath":   result.FilePath,
		"fileUrl":    result.FileURL,
		"postUrl":    result.PostURL,
		"postFields": result.PostFields,
	}
	if result.ExpiresAt > 0 {
		data["expiresAt"] = result.ExpiresAt
	}
	return respx.Envelope(data), nil
}
