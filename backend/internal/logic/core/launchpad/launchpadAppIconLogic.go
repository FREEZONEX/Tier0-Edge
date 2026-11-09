// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package launchpad

import (
	"context"

	"backend/internal/domain/launchpad"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadAppIconLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadAppIconLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadAppIconLogic {
	return &LaunchpadAppIconLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LaunchpadAppIcon 返回该 App 的图标资产 id；鉴权与归属校验在 domain 层完成，
// handler 拿到 id 后直接流式回图片（见 launchpadAppIconHandler.go）。
func (l *LaunchpadAppIconLogic) LaunchpadAppIcon(req *types.LaunchpadAppViewReq) (int64, error) {
	assetID, err := l.svcCtx.App.Launchpad.AppMediaAssetID(l.ctx, logicx.UserID(l.ctx), req.AppID, launchpad.MediaKindIcon)
	if err != nil {
		return 0, logicx.Error(err)
	}
	return assetID, nil
}
