// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	domainasset "backend/internal/domain/asset"
	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/infra/filestore"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssetMultipartCompleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetMultipartCompleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetMultipartCompleteLogic {
	return &AssetMultipartCompleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetMultipartCompleteLogic) AssetMultipartComplete(req *types.AssetMultipartCompleteReq) (resp *types.Envelope, err error) {
	userID := logicx.UserID(l.ctx)
	// 先取 temp 记录拿到所属项目，复检会话用户对该项目的管理权限（与 init 一致）：
	// 用户失去项目权限后不得再凭 fileKey/uploadId 完成/查看该项目作用域的分片对象。
	item, err := l.svcCtx.App.Asset.ResolveMultipartTemp(l.ctx, req.FileKey, req.UploadId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, item.ProjectID, userID); err != nil {
		return nil, logicx.Error(err)
	}
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
	// core 版 complete 成功后把 asset 绑定到所属项目（ownerType=projectApp、ownerId=projectId），
	// 镜像单发上传 UploadAttachment→Bind 的语义，使 ProjectAppImport 的 checkImportAssetOwned
	// 能通过归属校验；绑定失败返回错误，不静默。openapi 分片路径不走此绑定。
	if _, err := l.svcCtx.App.Asset.Bind(l.ctx, domainasset.BindCommand{
		FileID:    result.FileID,
		OwnerType: domainproject.ProjectImportAssetOwnerType,
		OwnerID:   item.ProjectID,
		UserID:    userID,
	}); err != nil {
		return nil, logicx.Error(err)
	}
	data := map[string]any{
		"fileId":    result.FileID,
		"filePath":  result.FilePath,
		"fileUrl":   result.FileURL,
		"sizeBytes": result.SizeBytes,
	}
	if result.ExpiresAt > 0 {
		data["expiresAt"] = result.ExpiresAt
	}
	RecordUploadAudit(l.ctx, l.svcCtx, map[string]any{
		"fileId": result.FileID, "fileName": item.OriginalName, "sizeBytes": result.SizeBytes,
	})
	return respx.Envelope(data), nil
}
