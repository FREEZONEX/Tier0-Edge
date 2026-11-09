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

type OpenapiAssetMultipartInitLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetMultipartInitLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetMultipartInitLogic {
	return &OpenapiAssetMultipartInitLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetMultipartInitLogic) OpenapiAssetMultipartInit(req *types.OpenapiAssetMultipartInitReq) (resp *types.Envelope, err error) {
	result, err := l.svcCtx.App.Asset.CreateMultipartUpload(l.ctx, domainasset.OpenapiMultipartInitCommand{
		FileName:      req.FileName,
		ContentType:   req.ContentType,
		Size:          req.Size,
		PartSize:      req.PartSize,
		Business:      req.Business,
		UseBy:         req.UseBy,
		Visibility:    req.Visibility,
		AppInstanceID: req.AppInstanceId,
		SessionID:     req.SessionId,
		BizID:         req.BizId,
		UserID:        logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, "", req.FileName, auditdomain.BizCreate, map[string]any{
		"fileName":  req.FileName,
		"fileKey":   result.FileKey,
		"filePath":  result.FilePath,
		"size":      req.Size,
		"partSize":  result.PartSize,
		"partCount": result.PartCount,
	})
	data := map[string]any{
		"fileKey":   result.FileKey,
		"filePath":  result.FilePath,
		"uploadId":  result.UploadID,
		"partSize":  result.PartSize,
		"partCount": result.PartCount,
	}
	if result.ExpiresAt > 0 {
		data["expiresAt"] = result.ExpiresAt
	}
	return respx.Envelope(data), nil
}
