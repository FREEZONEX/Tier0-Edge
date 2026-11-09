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

type AssetBindLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetBindLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetBindLogic {
	return &AssetBindLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetBindLogic) AssetBind(req *types.AssetBindReq) (resp *types.Envelope, err error) {
	ownerID := domainasset.ParseOwnerID(req.OwnerId)
	data, err := l.svcCtx.App.Asset.Bind(l.ctx, domainasset.BindCommand{
		FileID: req.FileId, OwnerType: req.OwnerType, OwnerID: ownerID,
		PermissionKey: req.PermissionKey, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, req.FileId, "", auditdomain.BizCreate, map[string]any{
		"ownerType": req.OwnerType,
		"ownerId":   ownerID,
	})
	return respx.Envelope(data), nil
}
