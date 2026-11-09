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

type AssetUploadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetUploadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetUploadLogic {
	return &AssetUploadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetUploadLogic) AssetUpload(req *types.AssetUploadReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Asset.Upload(l.ctx, domainasset.UploadCommand{
		FileName: req.FileName, ContentType: req.ContentType, Size: req.Size, Sha256: req.Sha256,
		StorageKey: req.StorageKey, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	RecordUploadAudit(l.ctx, l.svcCtx, data)
	return respx.Envelope(data), nil
}
