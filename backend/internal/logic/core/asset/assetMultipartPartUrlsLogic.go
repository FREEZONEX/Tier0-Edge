// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssetMultipartPartUrlsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetMultipartPartUrlsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetMultipartPartUrlsLogic {
	return &AssetMultipartPartUrlsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetMultipartPartUrlsLogic) AssetMultipartPartUrls(req *types.AssetMultipartPartUrlsReq) (resp *types.Envelope, err error) {
	// 先取 temp 记录拿到所属项目，复检会话用户对该项目的管理权限（与 init 一致）；
	// 校验放最前，通过后才签发分片 PUT 地址，避免向无权限用户泄露对象 key/URL。
	item, err := l.svcCtx.App.Asset.ResolveMultipartTemp(l.ctx, req.FileKey, req.UploadId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, item.ProjectID, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	results, err := l.svcCtx.App.Asset.MultipartPartURLs(l.ctx, req.FileKey, req.UploadId, req.PartNumbers)
	if err != nil {
		return nil, logicx.Error(err)
	}
	partUrls := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{
			"partNumber": r.PartNumber,
			"url":        r.URL,
		}
		if r.ExpiresAt > 0 {
			item["expiresAt"] = r.ExpiresAt
		}
		partUrls = append(partUrls, item)
	}
	return respx.Envelope(map[string]any{"partUrls": partUrls}), nil
}
