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

type OpenapiAssetMultipartAbortLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetMultipartAbortLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetMultipartAbortLogic {
	return &OpenapiAssetMultipartAbortLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OpenapiAssetMultipartAbortLogic) OpenapiAssetMultipartAbort(req *types.OpenapiAssetMultipartAbortReq) (resp *types.Envelope, err error) {
	aborted, err := l.svcCtx.App.Asset.MultipartAbort(l.ctx, req.FileKey, req.UploadId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, "", "", auditdomain.BizDelete, map[string]any{
		"fileKey":  req.FileKey,
		"uploadId": req.UploadId,
		"aborted":  aborted,
	})
	return respx.Envelope(map[string]any{"aborted": aborted}), nil
}
