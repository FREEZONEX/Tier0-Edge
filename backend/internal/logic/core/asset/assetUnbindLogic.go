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

type AssetUnbindLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetUnbindLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetUnbindLogic {
	return &AssetUnbindLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetUnbindLogic) AssetUnbind(req *types.IdReq) (resp *types.Envelope, err error) {
	// 解绑成功后绑定行不可查，先取绑定信息供审计 detail 使用；取不到不阻断解绑。
	binding, _ := l.svcCtx.App.Asset.Binding(l.ctx, req.Id)
	data, err := l.svcCtx.App.Asset.Unbind(l.ctx, req.Id)
	if err != nil {
		return nil, logicx.Error(err)
	}
	recordAssetAudit(l.ctx, l.svcCtx, binding.AssetID, "", auditdomain.BizDelete, map[string]any{
		"ownerType": binding.OwnerType,
		"ownerId":   binding.OwnerID,
	})
	return respx.Envelope(data), nil
}
