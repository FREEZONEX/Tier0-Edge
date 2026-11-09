// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	domainasset "backend/internal/domain/asset"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssetMultipartInitLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetMultipartInitLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetMultipartInitLogic {
	return &AssetMultipartInitLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetMultipartInitLogic) AssetMultipartInit(req *types.AssetMultipartInitReq) (resp *types.Envelope, err error) {
	userID := logicx.UserID(l.ctx)
	// core 会话版分片上传要求调用方具备该 project 的管理权限（平台 admin 或 project 创建者）。
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, req.ProjectId, userID); err != nil {
		return nil, logicx.Error(err)
	}
	result, err := l.svcCtx.App.Asset.CoreMultipartInit(l.ctx, domainasset.CoreMultipartInitCommand{
		ProjectID:   req.ProjectId,
		FileName:    req.FileName,
		ContentType: req.ContentType,
		Size:        req.Size,
		PartSize:    req.PartSize,
		UserID:      userID,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
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
